package pluginsdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk/ipc"
)

// Client is a thread-safe multiplexed UDS IPC client runtime.
type Client struct {
	conn       net.Conn
	writeMu    sync.Mutex
	seqCounter uint32

	pendingMu sync.Mutex
	pending   map[uint32]chan *ipc.Frame

	subMu       sync.RWMutex
	subscribers map[uint32]*metricsSubHandle
	nextSubID   uint32

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
	onShutdown   func()
	closed       atomic.Bool
	logger       Logger
}

type metricsSubHandle struct {
	id            uint32
	serverIDs     map[string]bool
	serverIDsList []string
	handler       MetricsHandler
	queue         chan *MetricsEvent
	stopCh        chan struct{}
	closed        atomic.Bool
	client        *Client
}

func (s *metricsSubHandle) ServerIDs() []string {
	return s.serverIDsList
}

func (s *metricsSubHandle) Close() error {
	if s.closed.Swap(true) {
		return nil
	}

	// Unsubscribe on host
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	unsubReq := ipc.UnsubscribeMetricsReq{ServerIDs: s.serverIDsList}
	_, _ = s.client.Call(ctx, ipc.TypeUnsubscribeMetricsReq, unsubReq, ipc.TypeUnsubscribeMetricsResp)

	// Remove from client registry
	s.client.subMu.Lock()
	delete(s.client.subscribers, s.id)
	s.client.subMu.Unlock()

	// Stop dispatcher loop
	close(s.stopCh)
	return nil
}

func (s *metricsSubHandle) dispatchLoop(logger Logger) {
	for {
		select {
		case <-s.stopCh:
			return
		case event, ok := <-s.queue:
			if !ok {
				return
			}
			if event != nil {
				s.handler.HandleMetrics(event)
			}
		}
	}
}

func newClient(conn net.Conn, logger Logger, onShutdown func()) *Client {
	if logger == nil {
		logger = NewDefaultLogger("client")
	}
	c := &Client{
		conn:        conn,
		pending:     make(map[uint32]chan *ipc.Frame),
		subscribers: make(map[uint32]*metricsSubHandle),
		shutdownCh:  make(chan struct{}),
		onShutdown:  onShutdown,
		logger:      logger,
	}
	go c.readLoop()
	return c
}

// Call executes a synchronous request-response RPC over the multiplexed UDS stream.
func (c *Client) Call(ctx context.Context, msgType ipc.MsgType, reqPayload any, respType ipc.MsgType) (*ipc.Frame, error) {
	if c.closed.Load() {
		return nil, ErrConnectionClosed
	}

	seq := atomic.AddUint32(&c.seqCounter, 1)

	var frame *ipc.Frame
	var err error
	if reqPayload != nil {
		if raw, ok := reqPayload.([]byte); ok {
			frame = ipc.NewFrame(msgType, seq, raw)
			frame.Flags.Set(ipc.FlagJSON)
		} else {
			frame, err = ipc.NewJSONFrame(msgType, seq, reqPayload)
			if err != nil {
				return nil, fmt.Errorf("marshal rpc payload: %w", err)
			}
		}
	} else {
		frame = ipc.NewFrame(msgType, seq, nil)
	}

	respCh := make(chan *ipc.Frame, 1)
	c.pendingMu.Lock()
	c.pending[seq] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, seq)
		c.pendingMu.Unlock()
	}()

	if err := c.writeFrame(frame); err != nil {
		if errors.Is(err, ErrConnectionClosed) {
			return nil, ErrConnectionClosed
		}
		return nil, fmt.Errorf("write rpc frame: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.shutdownCh:
		return nil, ErrConnectionClosed
	case resp, ok := <-respCh:
		if !ok || resp == nil {
			return nil, ErrConnectionClosed
		}
		if resp.Type == ipc.TypeErrorResp || resp.Flags.Has(ipc.FlagError) {
			var errPayload ipc.ErrorPayload
			_ = resp.UnmarshalPayload(&errPayload)
			return nil, &RPCError{Code: errPayload.Code, Message: errPayload.Error}
		}
		if respType != 0 && resp.Type != respType {
			return nil, fmt.Errorf("unexpected response type 0x%02X, expected 0x%02X", resp.Type, respType)
		}
		return resp, nil
	}
}

func (c *Client) writeFrame(f *ipc.Frame) error {
	if c.closed.Load() {
		return ErrConnectionClosed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return ErrConnectionClosed
	}
	if err := ipc.WriteFrame(c.conn, f); err != nil {
		if c.closed.Load() || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
			return ErrConnectionClosed
		}
		return err
	}
	return nil
}

func (c *Client) readLoop() {
	defer c.close()

	for {
		frame, err := ipc.ReadFrame(c.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !c.closed.Load() {
				c.logger.Errorf("read frame error: %v", err)
			}
			return
		}

		c.dispatchFrame(frame)
	}
}

func (c *Client) dispatchFrame(f *ipc.Frame) {
	switch f.Type {
	case ipc.TypePing:
		// Echo Pong with same SeqID and Payload
		pong := ipc.NewFrame(ipc.TypePong, f.SeqID, f.Payload)
		_ = c.writeFrame(pong)

	case ipc.TypeMetricsEvent:
		// Asynchronous metrics broadcast from host
		c.dispatchMetrics(f)

	case ipc.TypeShutdownReq:
		// Host asks plugin to terminate gracefully
		c.logger.Infof("Received TypeShutdownReq from host")
		ack := ipc.NewFrame(ipc.TypeShutdownResp, f.SeqID, []byte(`{"ready":true}`))
		ack.Flags.Set(ipc.FlagJSON)
		_ = c.writeFrame(ack)
		if c.onShutdown != nil {
			go c.onShutdown()
		}

	default:
		// Correlate with pending RPC
		c.pendingMu.Lock()
		ch, exists := c.pending[f.SeqID]
		if exists {
			delete(c.pending, f.SeqID)
		}
		c.pendingMu.Unlock()

		if exists && ch != nil {
			ch <- f
			close(ch)
		}
	}
}

func (c *Client) dispatchMetrics(f *ipc.Frame) {
	var event MetricsEvent
	if err := f.UnmarshalPayload(&event); err != nil {
		c.logger.Errorf("unmarshal metrics event error: %v", err)
		return
	}
	event.Raw = f.Payload
	if event.ReceivedAt.IsZero() {
		event.ReceivedAt = time.Now().UTC()
	}

	c.subMu.RLock()
	defer c.subMu.RUnlock()

	for _, sub := range c.subscribers {
		if len(sub.serverIDs) > 0 {
			targetID := event.Server.ID
			if targetID == "" && event.Stats != nil {
				targetID = event.Stats.ServerID
			}
			if targetID == "" {
				targetID = event.ServerID
			}
			if !sub.serverIDs[targetID] {
				continue
			}
		}
		select {
		case sub.queue <- &event:
		default:
			c.logger.Warnf("metrics queue full for subscriber %d, dropping event", sub.id)
		}
	}
}

// GetServerList queries the host for all monitored servers.
func (c *Client) GetServerList(ctx context.Context) ([]Server, error) {
	respFrame, err := c.Call(ctx, ipc.TypeGetServerListReq, nil, ipc.TypeGetServerListResp)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Servers []Server `json:"servers"`
	}
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return nil, fmt.Errorf("unmarshal servers list: %w", err)
	}
	return resp.Servers, nil
}

// GetServer queries the host for details of a specific server.
func (c *Client) GetServer(ctx context.Context, serverID string) (*Server, error) {
	req := ipc.GetServerReq{ServerID: serverID}
	respFrame, err := c.Call(ctx, ipc.TypeGetServerReq, req, ipc.TypeGetServerResp)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Server *Server `json:"server"`
	}
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return nil, fmt.Errorf("unmarshal server details: %w", err)
	}
	return resp.Server, nil
}

// SubscribeMetrics initiates real-time streaming of probe metrics from the host.
func (c *Client) SubscribeMetrics(ctx context.Context, opts SubscribeOptions, handler MetricsHandler) (MetricsSubscription, error) {
	req := ipc.SubscribeMetricsReq{
		ServerIDs:  opts.ServerIDs,
		IntervalMs: opts.IntervalMs,
	}
	respFrame, err := c.Call(ctx, ipc.TypeSubscribeMetricsReq, req, ipc.TypeSubscribeMetricsResp)
	if err != nil {
		return nil, err
	}
	var resp ipc.SubscribeMetricsResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return nil, fmt.Errorf("unmarshal subscribe resp: %w", err)
	}
	if !resp.Subscribed {
		return nil, fmt.Errorf("host rejected metrics subscription")
	}

	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = 256
	}

	subID := atomic.AddUint32(&c.nextSubID, 1)
	srvMap := make(map[string]bool, len(opts.ServerIDs))
	for _, id := range opts.ServerIDs {
		srvMap[id] = true
	}

	sub := &metricsSubHandle{
		id:            subID,
		serverIDs:     srvMap,
		serverIDsList: opts.ServerIDs,
		handler:       handler,
		queue:         make(chan *MetricsEvent, bufSize),
		stopCh:        make(chan struct{}),
		client:        c,
	}

	go sub.dispatchLoop(c.logger)

	c.subMu.Lock()
	c.subscribers[subID] = sub
	c.subMu.Unlock()

	return sub, nil
}

// SendNotification dispatches an alert notification through host notification channels.
func (c *Client) SendNotification(ctx context.Context, n Notification) error {
	req := ipc.SendNotificationReq{
		Title:    n.Title,
		Message:  n.Message,
		ServerID: n.ServerID,
		Details:  n.Details,
	}
	respFrame, err := c.Call(ctx, ipc.TypeSendNotificationReq, req, ipc.TypeSendNotificationResp)
	if err != nil {
		return err
	}
	var resp ipc.SendNotificationResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return fmt.Errorf("unmarshal notification resp: %w", err)
	}
	if !resp.Success {
		return &RPCError{Code: 500, Message: resp.Error}
	}
	return nil
}

func (c *Client) close() {
	c.shutdownOnce.Do(func() {
		c.closed.Store(true)
		close(c.shutdownCh)
		_ = c.conn.Close()

		// Unblock all pending RPCs
		c.pendingMu.Lock()
		for seq, ch := range c.pending {
			close(ch)
			delete(c.pending, seq)
		}
		c.pendingMu.Unlock()

		// Close all active subscribers
		c.subMu.Lock()
		for id, sub := range c.subscribers {
			if !sub.closed.Swap(true) {
				close(sub.stopCh)
			}
			delete(c.subscribers, id)
		}
		c.subMu.Unlock()
	})
}
