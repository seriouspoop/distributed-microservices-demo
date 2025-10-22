import React, { useState, useEffect, useRef } from "react";
import type { FormEvent } from "react"; // Type-only import
import axios from "axios";
import "./App.css";

// --- API & WS URLs ---
// Use ws:// for non-secure, wss:// for secure (https)
const API_URL = "http://localhost:3000/api";
const WS_URL = "ws://localhost:3000/api/ws";

// --- Type Definitions ---

// User object from the backend
interface User {
  id: string;
  email: string;
}

// Login response from backend
interface LoginResponse {
  token: string;
  user: User;
}

// Notification object from WebSocket
interface Notification {
  id: string;
  userId: string;
  message: string;
  timestamp: string; // This will be a string from JSON, we can format it.
}

// Props for sub-components
interface BillingInfoProps {
  billingAmount: number;
  onUpdateBilling: () => void;
}

interface NotificationsProps {
  notifications: Notification[];
}

// --- Main App Component ---

const App: React.FC = () => {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [loggedInUser, setLoggedInUser] = useState<User | null>(null);
  const [billingAmount, setBillingAmount] = useState<number | null>(null);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Ref to hold the WebSocket instance
  const ws = useRef<WebSocket | null>(null);

  // --- Effects ---

  // Effect for WebSocket connection
  useEffect(() => {
    if (loggedInUser) {
      // Fetch initial data
      fetchBillingInfo(loggedInUser.id);

      // Connect to WebSocket
      const socket = new WebSocket(`${WS_URL}?userId=${loggedInUser.id}`);
      ws.current = socket;

      socket.onopen = () => {
        console.log("WebSocket connected");
        // Add a local-only notification
        addLocalNotification("Real-time notifications connected.");
      };

      socket.onmessage = (event) => {
        try {
          const notification: Notification = JSON.parse(event.data);
          // Add the received notification to the state
          setNotifications((prev) => [notification, ...prev]);
        } catch (err) {
          console.error("Failed to parse incoming notification:", err);
        }
      };

      socket.onclose = () => {
        console.log("WebSocket disconnected");
        if (ws.current === socket) {
          // Only update if it's the current socket
          ws.current = null;
        }
      };

      socket.onerror = (err) => {
        console.error("WebSocket error:", err);
      };

      // Cleanup function on component unmount or user change
      return () => {
        console.log("Closing WebSocket connection...");
        socket.close();
        ws.current = null;
      };
    } else {
      // Clear data on logout
      setBillingAmount(null);
      setNotifications([]);
    }
  }, [loggedInUser]);

  // --- API Calls ---

  const fetchBillingInfo = async (userId: string) => {
    try {
      const response = await axios.get<{ amount: number }>(
        `${API_URL}/user/billing/${userId}`
      );
      if (response.data && typeof response.data.amount === "number") {
        setBillingAmount(response.data.amount);
      } else {
        console.error(
          "Billing info response is missing amount:",
          response.data
        );
        setBillingAmount(0); // Default to 0 if data is malformed
      }
    } catch (err) {
      console.error("Failed to fetch billing info:", err);
      setError("Could not fetch billing info.");
    }
  };

  const handleRegister = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      await axios.post(`${API_URL}/register`, { email, password });
      addLocalNotification(
        `User '${email}' registered successfully! Please log in.`
      );
      setEmail("");
      setPassword("");
    } catch (err) {
      console.error("Registration failed:", err);
      setError("Registration failed. Please try again.");
    }
  };

  const handleLogin = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      // Expect the new LoginResponse structure
      const response = await axios.post<LoginResponse>(`${API_URL}/login`, {
        email,
        password,
      });
      if (response.data && response.data.user) {
        setLoggedInUser(response.data.user);
      } else {
        throw new Error("Invalid login response from server");
      }
    } catch (err) {
      console.error("Login failed:", err);
      setError("Login failed. Please check your credentials.");
    }
  };

  const handleLogout = () => {
    addLocalNotification(`User ${loggedInUser?.email} logged out.`);
    setLoggedInUser(null);
    // The useEffect will handle closing the WebSocket
  };

  const handleUpdateBilling = async () => {
    if (!loggedInUser) return;
    try {
      await axios.post(`${API_URL}/user/billing/update`, {
        user_id: loggedInUser.id,
        amount: 10.5 + billingAmount!, // Increment by a fixed amount for demo
      });
      // Re-fetch billing info to show the update
      // A notification will also arrive via WebSocket
      fetchBillingInfo(loggedInUser.id);
    } catch (err) {
      console.error("Failed to update billing:", err);
      setError("Failed to update billing.");
    }
  };

  // --- Helper Functions ---

  // Adds a notification generated by the frontend
  const addLocalNotification = (message: string) => {
    const newNotif: Notification = {
      id: new Date().toISOString(),
      userId: loggedInUser?.id || "local",
      message,
      timestamp: new Date().toISOString(),
    };
    setNotifications((prev) => [newNotif, ...prev]);
  };

  // --- Render ---

  if (loggedInUser) {
    return (
      <div className="container logged-in">
        <h2>Welcome, {loggedInUser.email}</h2>
        <p className="user-id">User ID: {loggedInUser.id}</p>
        <button onClick={handleLogout} className="logout-button">
          Logout
        </button>
        <div className="content-area">
          {typeof billingAmount === "number" && (
            <BillingInfo
              billingAmount={billingAmount}
              onUpdateBilling={handleUpdateBilling}
            />
          )}
          <Notifications notifications={notifications} />
        </div>
      </div>
    );
  }

  return (
    <div className="container">
      <h2>User Billing System</h2>
      <div className="form-container">
        <form onSubmit={handleRegister}>
          <h3>Register</h3>
          <input
            type="text"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="Email"
            required
          />
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            required
          />
          <button type="submit">Register</button>
        </form>
        <form onSubmit={handleLogin}>
          <h3>Login</h3>
          <input
            type="text"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="Email"
            required
          />
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Password"
            required
          />
          <button type="submit">Login</button>
        </form>
      </div>
      {error && <p className="error-message">{error}</p>}
      {/* Show notifications on login page for registration success, etc. */}
      <Notifications notifications={notifications} />
    </div>
  );
};

// --- Sub-components ---

const BillingInfo: React.FC<BillingInfoProps> = ({
  billingAmount,
  onUpdateBilling,
}) => {
  return (
    <div className="card billing-card">
      <h3>Billing Information</h3>
      <p>Current Amount Due:</p>
      <p className="text-2xl font-bold">${(billingAmount ?? 0).toFixed(2)}</p>
      <button onClick={onUpdateBilling}>Increment Bill by $10.50</button>
    </div>
  );
};

const Notifications: React.FC<NotificationsProps> = ({ notifications }) => {
  const formatTimestamp = (ts: string) => {
    // The timestamp from gRPC might be a full object, but from JSON it's a string
    try {
      return new Date(ts).toLocaleTimeString();
    } catch {
      return "just now";
    }
  };

  return (
    <div className="card notifications-card">
      <h3>Notifications</h3>
      <div className="notifications-list">
        {notifications.length === 0 && <p>No new notifications.</p>}
        {notifications.map((notif) => (
          <div key={notif.id} className="notification-item">
            <p>{notif.message}</p>
            <span className="timestamp">
              {formatTimestamp(notif.timestamp)}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
};

export default App;
