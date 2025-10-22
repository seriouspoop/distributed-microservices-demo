package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"api-gateway/billingpb"
	"api-gateway/notifpb"
	"api-gateway/userpb"
)

// --- WebSocket Upgrader ---
var upgrader = websocket.Upgrader{
	// Allow all origins
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// apiServer holds the dependencies for our HTTP handlers.
type apiServer struct {
	userClient    userpb.UserServiceClient
	billingClient billingpb.BillingServiceClient
	notifClient   notifpb.NotificationServiceClient
	router        *http.ServeMux
	logger        *slog.Logger
}

// newAPIServer creates a new instance of our server.
func newAPIServer(userClient userpb.UserServiceClient, billingClient billingpb.BillingServiceClient, notifClient notifpb.NotificationServiceClient, logger *slog.Logger) *apiServer {
	s := &apiServer{
		userClient:    userClient,
		billingClient: billingClient,
		notifClient:   notifClient,
		router:        http.NewServeMux(),
		logger:        logger,
	}
	s.routes()
	return s
}

// ServeHTTP makes our apiServer implement the http.Handler interface.
func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// routes sets up all the application's routes.
func (s *apiServer) routes() {
	s.router.HandleFunc("POST /register", s.handleRegister())
	s.router.HandleFunc("POST /login", s.handleLogin())
	s.router.HandleFunc("GET /user/billing/{user_id}", s.handleGetBillingInfo())
	s.router.HandleFunc("POST /user/billing/update", s.handleUpdateBilling())
	s.router.HandleFunc("GET /ws", s.handleWebSocket())
}

func main() {
	// --- Structured Logger Setup ---
	// Initialize a new JSON-based logger that writes to standard output.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// --- gRPC Client Connections ---
	userConn, err := grpc.NewClient("user-ms:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("did not connect to user service", "error", err)
		os.Exit(1)
	}
	defer userConn.Close()
	userClient := userpb.NewUserServiceClient(userConn)

	billingConn, err := grpc.NewClient("billing-ms:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("did not connect to billing service", "error", err)
		os.Exit(1)
	}
	defer billingConn.Close()
	billingClient := billingpb.NewBillingServiceClient(billingConn)

	notifConn, err := grpc.NewClient("notification-ms:50053", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Error("did not connect to notification service", "error", err)
		os.Exit(1)
	}
	defer notifConn.Close()
	notifClient := notifpb.NewNotificationServiceClient(notifConn)

	// --- HTTP Server Setup ---
	server := newAPIServer(userClient, billingClient, notifClient, logger)
	// Wrap the main handler with logging and then CORS middleware.
	handler := loggingMiddleware(logger)(corsMiddleware(server))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("API Gateway starting", "port", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		logger.Error("failed to start server", "error", err)
		os.Exit(1)
	}
}

// --- WebSocket Handler ---

func (s *apiServer) handleWebSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.URL.Query().Get("userId")
		if userID == "" {
			s.logger.Warn("WebSocket connection attempt with no userId")
			s.writeJSONError(w, http.StatusBadRequest, "userId query parameter is required")
			return
		}

		// Upgrade the HTTP connection to a WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			s.logger.Error("failed to upgrade websocket", "error", err)
			return
		}
		defer conn.Close()
		s.logger.Info("WebSocket connected", "user_id", userID)

		// Create a context for the gRPC stream
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Call the gRPC stream on the Notification service
		stream, err := s.notifClient.SubscribeToNotifications(ctx, &notifpb.SubscribeRequest{UserId: userID})
		if err != nil {
			s.logger.Error("failed to subscribe to notifications", "error", err)
			return
		}

		// Goroutine to read from gRPC stream and write to WebSocket
		go func() {
			for {
				notification, err := stream.Recv()
				if err != nil {
					// Handle stream ending or error
					if err == io.EOF {
						s.logger.Info("gRPC stream closed by server", "user_id", userID)
					} else if ctx.Err() != nil {
						// Check if the context was cancelled (client disconnected)
						s.logger.Info("gRPC stream cancelled by client", "user_id", userID)
					} else {
						s.logger.Error("gRPC stream error", "error", err, "user_id", userID)
					}
					conn.WriteMessage(websocket.CloseMessage, []byte("Stream closed"))
					return
				}

				// Write the notification to the WebSocket as JSON
				s.logger.Info("Sending notification to WebSocket", "user_id", userID)
				if err := conn.WriteJSON(notification); err != nil {
					s.logger.Error("failed to write message to websocket", "error", err)
					return
				}
			}
		}()

		// Read loop to detect when the WebSocket client disconnects
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					s.logger.Error("websocket read error", "error", err)
				} else {
					s.logger.Info("WebSocket disconnected", "user_id", userID)
				}
				cancel() // Cancel the gRPC stream context
				break
			}
		}
	}
}

// --- Route Handlers ---

func (s *apiServer) handleRegister() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req userpb.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		res, err := s.userClient.Register(r.Context(), &req)
		if err != nil {
			s.logger.Error("failed to register user", "error", err)
			s.writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, res)
	}
}

func (s *apiServer) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req userpb.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		res, err := s.userClient.Login(r.Context(), &req)
		if err != nil {
			if strings.Contains(err.Error(), "invalid credentials") {
				s.writeJSONError(w, http.StatusUnauthorized, err.Error())
			} else {
				s.logger.Error("failed during login", "error", err)
				s.writeJSONError(w, http.StatusInternalServerError, "An internal error occurred")
			}
			return
		}
		s.writeJSON(w, http.StatusOK, res)
	}
}

func (s *apiServer) handleGetBillingInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("user_id")
		if userID == "" {
			s.writeJSONError(w, http.StatusBadRequest, "User ID is required")
			return
		}

		req := &billingpb.GetBillingRequest{UserId: userID}
		res, err := s.billingClient.GetBilling(r.Context(), req)
		if err != nil {
			s.logger.Error("failed to get billing info", "user_id", userID, "error", err)
			s.writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, res)
	}
}

func (s *apiServer) handleUpdateBilling() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req billingpb.UpdateBillingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		res, err := s.billingClient.UpdateBilling(r.Context(), &req)
		if err != nil {
			s.logger.Error("failed to update billing", "user_id", req.UserId, "error", err)
			s.writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, res)
	}
}

// --- Helper Functions & Middleware ---

func (s *apiServer) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("error encoding JSON", "error", err)
	}
}

func (s *apiServer) writeJSONError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs details about each incoming HTTP request.
func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			duration := time.Since(start)

			logger.Info("handled request",
				"method", r.Method,
				"path", r.URL.Path,
				"duration", duration,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}
