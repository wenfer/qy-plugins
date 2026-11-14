package pluginsdk

import (
	"context"
	"encoding/json"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk/ipc"
)

type storageClient struct {
	client *Client
}

func newStorageClient(c *Client) *storageClient {
	return &storageClient{client: c}
}

func (s *storageClient) Get(ctx context.Context, key string) (string, bool, error) {
	req := ipc.StorageGetReq{Key: key}
	respFrame, err := s.client.Call(ctx, ipc.TypeStorageGetReq, req, ipc.TypeStorageGetResp)
	if err != nil {
		return "", false, err
	}
	var resp ipc.StorageGetResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return "", false, err
	}
	return resp.Value, resp.Found, nil
}

func (s *storageClient) Set(ctx context.Context, key, value string) error {
	req := ipc.StorageSetReq{Key: key, Value: value}
	respFrame, err := s.client.Call(ctx, ipc.TypeStorageSetReq, req, ipc.TypeStorageSetResp)
	if err != nil {
		return err
	}
	var resp ipc.StorageSetResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return err
	}
	if !resp.Success {
		return &RPCError{Code: 500, Message: resp.Error}
	}
	return nil
}

func (s *storageClient) Delete(ctx context.Context, key string) error {
	req := ipc.StorageDeleteReq{Key: key}
	respFrame, err := s.client.Call(ctx, ipc.TypeStorageDeleteReq, req, ipc.TypeStorageDeleteResp)
	if err != nil {
		return err
	}
	var resp ipc.StorageDeleteResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return err
	}
	if !resp.Success {
		return &RPCError{Code: 500, Message: resp.Error}
	}
	return nil
}

func (s *storageClient) List(ctx context.Context, prefix string) (map[string]string, error) {
	req := ipc.StorageListReq{Prefix: prefix}
	respFrame, err := s.client.Call(ctx, ipc.TypeStorageListReq, req, ipc.TypeStorageListResp)
	if err != nil {
		return nil, err
	}
	var resp ipc.StorageListResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return nil, err
	}
	if resp.Items == nil {
		return make(map[string]string), nil
	}
	return resp.Items, nil
}

func (s *storageClient) Keys(ctx context.Context, prefix string) ([]string, error) {
	req := ipc.StorageListReq{Prefix: prefix}
	respFrame, err := s.client.Call(ctx, ipc.TypeStorageListReq, req, ipc.TypeStorageListResp)
	if err != nil {
		return nil, err
	}
	var resp ipc.StorageListResp
	if err := respFrame.UnmarshalPayload(&resp); err != nil {
		return nil, err
	}
	if resp.Keys == nil {
		return []string{}, nil
	}
	return resp.Keys, nil
}

func (s *storageClient) GetBytes(ctx context.Context, key string) ([]byte, bool, error) {
	val, found, err := s.Get(ctx, key)
	if err != nil || !found {
		return nil, found, err
	}
	return []byte(val), true, nil
}

func (s *storageClient) SetBytes(ctx context.Context, key string, value []byte) error {
	return s.Set(ctx, key, string(value))
}

func (s *storageClient) GetJSON(ctx context.Context, key string, v any) (bool, error) {
	val, found, err := s.Get(ctx, key)
	if err != nil || !found {
		return found, err
	}
	return true, json.Unmarshal([]byte(val), v)
}

func (s *storageClient) SetJSON(ctx context.Context, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(ctx, key, string(data))
}
