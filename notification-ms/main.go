package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notification-ms/notifpb"
)

// subscriber holds the channel for sending notifications to a specific stream
type subscriber struct {
	ch     chan *notifpb.Notification
	userId string
}

// notificationServer implements the gRPC server and manages active subscribers
type notificationServer struct {
	notifpb.UnimplementedNotificationServiceServer
	nc          *nats.Conn
	subscribers map[string]*subscriber
	mu          sync.RWMutex // Protects the subscribers map
}

// UserCreatedEvent matches the event from user-ms
type UserCreatedEvent struct {
	UID      string `json:"uid"`
	Username string `json:"username"`
}

// billUpdate matches the event you defined
type billUpdate struct {
	Id      string `json:"Id"`
	Message string `json:"Message"`
}

func main() {
	// --- NATS Connection ---
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://nats:4222" // Fallback for local dev
		log.Printf("NATS_URL not set, using default: %s", natsURL)
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// --- gRPC Server Setup ---
	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	server := &notificationServer{
		nc:          nc,
		subscribers: make(map[string]*subscriber),
	}
	notifpb.RegisterNotificationServiceServer(s, server)

	// Start NATS subscribers in a goroutine
	go server.subscribeToEvents()

	// Start gRPC server in a goroutine
	go func() {
		log.Println("Notification service is running on port :50053")
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// --- Graceful Shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down gRPC server...")
	s.GracefulStop()
	log.Println("gRPC server stopped.")
}

// subscribeToEvents listens for all NATS events
func (s *notificationServer) subscribeToEvents() {
	// Subscribe to user.created
	_, err := s.nc.Subscribe("user.created", func(m *nats.Msg) {
		log.Printf("Received user.created event: %s", string(m.Data))
		var event UserCreatedEvent
		if err := json.Unmarshal(m.Data, &event); err != nil {
			log.Printf("error unmarshalling user created event: %v", err)
			return
		}

		notif := &notifpb.Notification{
			Id:        uuid.New().String(),
			UserId:    event.UID,
			Message:   fmt.Sprintf("Welcome to the platform, %s!", event.Username),
			Timestamp: timestamppb.Now().AsTime().String(),
		}
		s.broadcast(event.UID, notif)
	})
	if err != nil {
		log.Printf("failed to subscribe to user.created: %v", err)
	}

	// Subscribe to bill.update
	_, err = s.nc.Subscribe("bill.update", func(msg *nats.Msg) {
		log.Printf("Received bill.update event: %s", string(msg.Data))
		var event billUpdate
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Println("error while unmarshalling bill.update")
			return
		}

		notif := &notifpb.Notification{
			Id:        uuid.New().String(),
			UserId:    event.Id,
			Message:   event.Message,
			Timestamp: timestamppb.Now().AsTime().String(),
		}
		s.broadcast(event.Id, notif)
	})
	if err != nil {
		log.Printf("failed to subscribe to bill.update: %v", err)
	}

	// Keep the goroutine alive
	select {}
}

// SubscribeToNotifications is the gRPC streaming method called by the API Gateway
func (s *notificationServer) SubscribeToNotifications(req *notifpb.SubscribeRequest, stream notifpb.NotificationService_SubscribeToNotificationsServer) error {
	userID := req.UserId
	log.Printf("New subscriber for user: %s", userID)

	// Create a new subscriber
	sub := &subscriber{
		ch:     make(chan *notifpb.Notification, 10), // Buffered channel
		userId: userID,
	}

	// Add to the map
	s.mu.Lock()
	s.subscribers[userID] = sub
	s.mu.Unlock()

	// Defer removal from map on disconnect
	defer func() {
		s.mu.Lock()
		delete(s.subscribers, userID)
		s.mu.Unlock()
		close(sub.ch)
		log.Printf("Subscriber disconnected for user: %s", userID)
	}()

	// Send loop: wait for new notifications on the channel or client disconnect
	for {
		select {
		case notif := <-sub.ch:
			// Send notification to the client stream
			if err := stream.Send(notif); err != nil {
				log.Printf("Error sending to stream for user %s: %v", userID, err)
				return err
			}
		case <-stream.Context().Done():
			// Client disconnected
			log.Printf("Client disconnected (context done) for user: %s", userID)
			return stream.Context().Err()
		}
	}
}

// broadcast sends a notification to a user's active streams
func (s *notificationServer) broadcast(userID string, notif *notifpb.Notification) {
	s.mu.RLock()
	sub, ok := s.subscribers[userID]
	s.mu.RUnlock()

	if !ok {
		log.Printf("No active subscribers for user %s, notification not sent in real-time.", userID)
		return
	}

	// Send to the channel in a non-blocking way
	select {
	case sub.ch <- notif:
		log.Printf("Successfully broadcasted notification to user: %s", userID)
	case <-time.After(1 * time.Second):
		// This can happen if the channel buffer is full and blocked
		log.Printf("Subscriber channel full for user %s, dropping notification.", userID)
	}
}
