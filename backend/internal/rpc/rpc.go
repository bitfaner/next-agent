package rpc

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      any          `json:"id,omitempty"`
	Result  any          `json:"result,omitempty"`
	Error   *ErrorObject `json:"error,omitempty"`
}

type ErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Handler func(*http.Request, json.RawMessage) (any, error)

type Server struct {
	methods map[string]Handler
}

func NewServer() *Server {
	return &Server{methods: make(map[string]Handler)}
}

func (s *Server) Register(method string, handler Handler) {
	s.methods[method] = handler
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		write(w, Response{JSONRPC: "2.0", Error: &ErrorObject{Code: -32600, Message: "POST required"}})
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, Response{JSONRPC: "2.0", Error: &ErrorObject{Code: -32700, Message: err.Error()}})
		return
	}
	handler, ok := s.methods[req.Method]
	if !ok {
		write(w, Response{JSONRPC: "2.0", ID: req.ID, Error: &ErrorObject{Code: -32601, Message: "method not found"}})
		return
	}
	result, err := handler(r, req.Params)
	if err != nil {
		write(w, Response{JSONRPC: "2.0", ID: req.ID, Error: &ErrorObject{Code: -32000, Message: err.Error()}})
		return
	}
	write(w, Response{JSONRPC: "2.0", ID: req.ID, Result: result})
}

func Decode[T any](params json.RawMessage) (T, error) {
	var value T
	if len(params) == 0 {
		return value, nil
	}
	if err := json.Unmarshal(params, &value); err != nil {
		return value, err
	}
	return value, nil
}

func RequiredID(id int64, name string) error {
	if id <= 0 {
		return errors.New(name + " is required")
	}
	return nil
}

func write(w http.ResponseWriter, resp Response) {
	_ = json.NewEncoder(w).Encode(resp)
}
