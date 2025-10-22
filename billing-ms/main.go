package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"

	"billing-ms/billingpb"
)

type server struct {
	billingpb.UnimplementedBillingServiceServer
	db *sql.DB
	nc *nats.Conn
}

type UserCreatedEvent struct {
	UID      string `json:"uid"`
	Username string `json:"username"`
}

func (s *server) CreateBillingAccount(ctx context.Context, req *billingpb.CreateBillingAccountRequest) (*billingpb.CreateBillingAccountResponse, error) {
	_, err := s.db.Exec("INSERT INTO billing (user_id, amount) VALUES ($1, $2)", req.UserId, 0.0)
	if err != nil {
		return nil, fmt.Errorf("could not create billing account: %v", err)
	}
	return &billingpb.CreateBillingAccountResponse{Success: true}, nil
}

func (s *server) GetBilling(ctx context.Context, req *billingpb.GetBillingRequest) (*billingpb.GetBillingResponse, error) {
	var amount float64
	err := s.db.QueryRow("SELECT amount FROM billing WHERE user_id = $1", req.UserId).Scan(&amount)
	if err != nil {
		return nil, fmt.Errorf("could not get billing: %v", err)
	}
	return &billingpb.GetBillingResponse{Amount: amount}, nil
}

func (s *server) UpdateBilling(ctx context.Context, req *billingpb.UpdateBillingRequest) (*billingpb.UpdateBillingResponse, error) {
	_, err := s.db.Exec("UPDATE billing SET amount = $1 WHERE user_id = $2", req.Amount, req.UserId)
	if err != nil {
		return nil, fmt.Errorf("could not update billing: %v", err)
	}

	msg := struct {
		Id      string
		Message string
	}{
		req.UserId,
		fmt.Sprintf("Your bill was updated to %.2f", req.Amount),
	}

	if req.Amount > 100 {
		msg.Message = fmt.Sprintf("Dr. Prakash Metre made you poor!! bill updated to %.2f", req.Amount)
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		log.Println("marshalling error")
		return nil, fmt.Errorf("internal server")
	}
	// Send notification
	s.nc.Publish("bill.update", msgBytes)

	return &billingpb.UpdateBillingResponse{Success: true}, nil
}

func main() {
	// Database connection
	connStr := "user=postgres password=postgres dbname=billingdb sslmode=disable host=postgres"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	// NATS connection
	nc, err := nats.Connect("nats:4222")
	if err != nil {
		log.Fatalf("failed to connect to nats: %v", err)
	}
	defer nc.Close()

	// Create table if not exists
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS billing (user_id TEXT PRIMARY KEY, amount REAL)`)
	if err != nil {
		log.Fatalf("failed to create table: %v", err)
	}

	// NATS subscription
	nc.Subscribe("user.created", func(m *nats.Msg) {
		var event UserCreatedEvent
		if err := json.Unmarshal(m.Data, &event); err != nil {
			log.Printf("error unmarshalling user created event: %v", err)
			return
		}
		log.Printf("Received new user: %s", event.UID)
		_, err := db.Exec("INSERT INTO billing (user_id, amount) VALUES ($1, $2)", event.UID, 0.0)
		if err != nil {
			log.Printf("failed to create billing account for user %s: %v", event.UID, err)
		}
	})

	// gRPC client for notification service
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	billingpb.RegisterBillingServiceServer(s, &server{db: db, nc: nc})
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
