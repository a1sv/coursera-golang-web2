package main

// тут вы пишете код
// обращаю ваше внимание - в этом задании запрещены глобальные переменные

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type adminServer struct {
	UnimplementedAdminServer

	subs *eventSubs
}

type logicServer struct {
	UnimplementedServer
}

func (b *logicServer) Check(context.Context, *Nothing) (*Nothing, error) {
	return &Nothing{}, nil
}

func (b *logicServer) Add(context.Context, *Nothing) (*Nothing, error) {
	return &Nothing{}, nil
}

func (b *logicServer) Test(context.Context, *Nothing) (*Nothing, error) {
	return &Nothing{}, nil
}

func newLogicServer() *logicServer {
	return &logicServer{}
}

func (a *adminServer) Logging(_ *Nothing, srv Admin_LoggingServer) error {
	id, events := a.subs.NewSub()
	defer a.subs.RemoveSub(id)

	for e := range events {
		if err := srv.Send(e); err != nil {
			return err
		}
	}

	return nil
}

func (a *adminServer) Statistics(i *StatInterval, srv Admin_StatisticsServer) error {
	id, events := a.subs.NewSub()
	defer a.subs.RemoveSub(id)

	stat := newStatCollector()
	t := time.NewTicker(time.Duration(i.IntervalSeconds) * time.Second)
	defer t.Stop()

	for {
		select {
		case e, ok := <-events:
			if ok {
				stat.Update(e)
			} else {
				return nil
			}
		case <-t.C:
			if err := srv.Send(stat.Collect()); err != nil {
				return err
			}
		}
	}
}

func newAdminServer(subs *eventSubs) *adminServer {
	return &adminServer{
		subs: subs,
	}
}

type aclMethods [][]string

type aclAuth struct {
	acl map[string]aclMethods
}

func (a *aclAuth) isAllowed(consumer string, method string) bool {
	methodParts := strings.Split(method, "/")

	if allowedMethods, ok := a.acl[consumer]; ok {
	nextMethod:
		for _, allowedMethod := range allowedMethods {
			for i, p := range allowedMethod {
				if len(methodParts) > i && (methodParts[i] == p || p == "*") {
					continue
				}

				break nextMethod
			}

			return true
		}
	}

	return false
}

func newAclAuth(aclData string) (*aclAuth, error) {
	aclRecords := make(map[string][]string)
	if err := json.Unmarshal([]byte(aclData), &aclRecords); err != nil {
		return nil, fmt.Errorf("failed to parse ACL data: %s", err)
	}

	auth := &aclAuth{
		acl: make(map[string]aclMethods, len(aclRecords)),
	}

	for consumer, methods := range aclRecords {
		auth.acl[consumer] = make(aclMethods, len(methods))
		for i, method := range methods {
			auth.acl[consumer][i] = strings.Split(method, "/")
		}
	}

	return auth, nil
}

type eventSubs struct {
	id   int
	subs map[int]chan *Event
	mux  *sync.RWMutex
}

func (s *eventSubs) Notify(e *Event) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	for _, sub := range s.subs {
		sub <- e
	}
}

func (s *eventSubs) NewSub() (int, chan *Event) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.id++
	s.subs[s.id] = make(chan *Event)
	return s.id, s.subs[s.id]
}

func (s *eventSubs) RemoveSub(id int) {
	s.mux.Lock()
	defer s.mux.Unlock()

	if sub, ok := s.subs[id]; ok {
		close(sub)
		delete(s.subs, id)
	}
}

func (s *eventSubs) RemoveAll() {
	for id, _ := range s.subs {
		s.RemoveSub(id)
	}
}

func newEventSubs() *eventSubs {
	return &eventSubs{
		subs: make(map[int]chan *Event),
		mux:  &sync.RWMutex{},
	}
}

type statCollector struct {
	stat Stat
}

func (s *statCollector) reset() {
	s.stat = Stat{
		ByMethod:   make(map[string]uint64),
		ByConsumer: make(map[string]uint64),
	}
}

func (s *statCollector) Collect() *Stat {
	stat := s.stat
	stat.Timestamp = time.Now().Unix()
	s.reset()

	return &stat
}

func (s *statCollector) Update(e *Event) {
	s.stat.ByConsumer[e.Consumer] += 1
	s.stat.ByMethod[e.Method] += 1
}

func newStatCollector() *statCollector {
	s := &statCollector{}
	s.reset()

	return s
}

type middleware struct {
	ServerOptions []grpc.ServerOption
	auth          *aclAuth
	subs          *eventSubs
}

func (m *middleware) process(ctx context.Context, method string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	consumer := strings.Join(md.Get("consumer"), "")
	host := ""
	if p, ok := peer.FromContext(ctx); ok {
		host = p.Addr.String()
	}

	m.subs.Notify(&Event{
		Method:    method,
		Consumer:  consumer,
		Host:      host,
		Timestamp: time.Now().Unix(),
	})

	if !m.auth.isAllowed(consumer, method) {
		return status.Errorf(codes.Unauthenticated, "access denied")
	}

	return nil
}

func (m *middleware) unaryInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	if err := m.process(ctx, info.FullMethod); err != nil {
		return nil, err
	}

	return handler(ctx, req)
}

func (m *middleware) streamInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	if err := m.process(ss.Context(), info.FullMethod); err != nil {
		return err
	}

	return handler(srv, ss)
}

func newMiddleware(auth *aclAuth, subs *eventSubs) *middleware {
	mid := &middleware{
		auth: auth,
		subs: subs,
	}

	mid.ServerOptions = []grpc.ServerOption{
		grpc.UnaryInterceptor(mid.unaryInterceptor),
		grpc.StreamInterceptor(mid.streamInterceptor),
	}

	return mid
}

func StartMyMicroservice(ctx context.Context, listenAddr string, aclData string) error {
	aclAuth, err := newAclAuth(aclData)
	if err != nil {
		return fmt.Errorf("failed to start service: %s", err)
	}

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to start service: %s", err)
	}

	subs := newEventSubs()
	mid := newMiddleware(aclAuth, subs)
	server := grpc.NewServer(mid.ServerOptions...)

	RegisterLogicServer(server, newLogicServer())
	RegisterAdminServer(server, newAdminServer(subs))

	go server.Serve(lis)
	go func() {
		<-ctx.Done()
		subs.RemoveAll()
		server.GracefulStop()
	}()

	return nil
}
