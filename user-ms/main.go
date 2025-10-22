package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"

	"user-ms/userpb"
)

type server struct {
	userpb.UnimplementedUserServiceServer
	db *sql.DB
	nc *nats.Conn
}

type UserCreatedEvent struct {
	UID      string `json:"uid"`
	Username string `json:"username"`
}

func (s *server) Register(ctx context.Context, req *userpb.RegisterRequest) (*userpb.RegisterResponse, error) {
	if req.Email == "" || req.Password == "" {
		log.Println("email or password not present")
		return nil, fmt.Errorf("bad input")
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("failed to hash password: %v", err)
		return nil, fmt.Errorf("internal server error")
	}

	userID := uuid.New().String()

	// Store the hashed password (as a string) in the database
	_, err = s.db.Exec("INSERT INTO users (id, email, password) VALUES ($1, $2, $3)", userID, req.Email, string(hashedPassword))
	if err != nil {
		return nil, fmt.Errorf("could not register user: %v", err)
	}

	eventMsg := &UserCreatedEvent{
		UID:      userID,
		Username: req.Email,
	}

	bytes, err := json.Marshal(eventMsg)
	if err != nil {
		log.Println("marshalling error")
		return nil, fmt.Errorf("internal server")
	}

	// Publish message to NATS
	s.nc.Publish("user.created", bytes)

	return &userpb.RegisterResponse{UserId: userID}, nil
}

func (s *server) Login(ctx context.Context, req *userpb.LoginRequest) (*userpb.LoginResponse, error) {
	var uid, hashedPassword string

	// Retrieve user from the database
	err := s.db.QueryRow("SELECT id, password FROM users WHERE email = $1", req.Email).Scan(&uid, &hashedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid credentials")
		}
		log.Printf("failed to query user: %v", err)
		return nil, fmt.Errorf("internal server error")
	}

	// Compare the password with the hash
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Password))
	if err != nil {
		// Passwords don't match
		return nil, fmt.Errorf("invalid credentials")
	}

	// --- Login successful, create response ---
	// (In a real app, you would generate a JWT token here)
	token := "sample-jwt-token-for-" + uid

	user := &userpb.User{
		Id:    uid,
		Email: req.Email,
	}

	log.Println("user built")

	return &userpb.LoginResponse{
		Token: token,
		User:  user,
	}, nil
}

func main() {
	// Database connection
	connStr := "user=postgres password=postgres dbname=userdb sslmode=disable host=postgres"
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
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, email TEXT, password TEXT)`)
	if err != nil {
		log.Fatalf("failed to create table: %v", err)
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	userpb.RegisterUserServiceServer(s, &server{db: db, nc: nc})
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
