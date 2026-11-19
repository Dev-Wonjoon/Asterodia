package webserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Dev-Wonjoon/StorageBucket/internal/api"
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
)

const DefaultAddress = "127.0.0.1:9246"

type Status struct {
	Running bool    `json:"running"`
	URL     string  `json:"url"`
	Error   *string `json:"error"`
}

type Service struct {
	mu         sync.Mutex
	library    *media.Service
	address    string
	server     *http.Server
	handler    http.Handler
	url        string
	lastError  *string
	closed     bool
	generation uint64
	requests   sync.WaitGroup
	capacity   chan struct{}
	cancel     context.CancelFunc
}

func NewService(library *media.Service, address string) *Service {
	if address == "" {
		address = DefaultAddress
	}
	s := &Service{library: library, address: address, url: "http://" + address, capacity: make(chan struct{}, 32)}
	s.handler = s.routes()
	return s
}

func (s *Service) snapshot() Status {
	return Status{Running: s.server != nil, URL: s.url, Error: s.lastError}
}

func (s *Service) Status() api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return api.Success(s.snapshot())
}

func (s *Service) Start() api.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return api.Failure("NOT_READY", "앱을 종료하고 있습니다")
	}
	if s.server != nil {
		return api.Success(s.snapshot())
	}
	if s.library == nil {
		return api.Failure("NOT_READY", "라이브러리를 사용할 준비가 되지 않았습니다")
	}
	host, _, err := net.SplitHostPort(s.address)
	if err != nil || host != "127.0.0.1" {
		return api.Failure("INVALID_ARGUMENT", "서버는 127.0.0.1에만 연결할 수 있습니다")
	}
	listener, err := net.Listen("tcp4", s.address)
	if err != nil {
		message := fmt.Sprintf("로컬 웹 서버를 시작하지 못했습니다: %v", err)
		s.lastError = &message
		return api.Failure("SERVER_START_FAILED", message)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.url = "http://" + listener.Addr().String()
	s.lastError = nil
	s.generation++
	generation := s.generation
	server := &http.Server{Handler: http.HandlerFunc(s.serveHTTP), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	s.server = server
	go func() {
		err := server.Serve(listener)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.generation == generation && s.server == server {
			s.server = nil
			cancel()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				message := "로컬 웹 서버 연결이 종료됐습니다"
				s.lastError = &message
			}
		}
	}()
	return api.Success(s.snapshot())
}

func (s *Service) Stop() api.Result {
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.generation++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	// Keep the lifecycle lock until requests finish so another Start cannot race Wait.
	if server != nil {
		_ = server.Close()
	}
	s.requests.Wait()
	status := s.snapshot()
	s.mu.Unlock()
	return api.Success(status)
}

//wails:ignore
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Stop()
}
