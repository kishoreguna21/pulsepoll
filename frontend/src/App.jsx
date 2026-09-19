import { useEffect, useMemo, useState } from "react";
import { Navigate, Route, Routes, Link, useLocation, useNavigate, useParams } from "react-router-dom";

const API_BASE = "https://pulsepoll-ypsp.onrender.com";
const TOKEN_KEY = "pulsepoll_token";
const USER_KEY = "pulsepoll_user";

const getStoredToken = () => localStorage.getItem(TOKEN_KEY);
const getStoredUser = () => {
  const raw = localStorage.getItem(USER_KEY);
  try {
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  
  }
};

const formatTime = (value) => {
  if (!value) return "Just now";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Just now" : date.toLocaleString();
};

function App() {
  const [token, setToken] = useState(getStoredToken());
  const [user, setUser] = useState(getStoredUser());
  const [flash, setFlash] = useState("");

  const saveSession = (authToken, authUser) => {
    setToken(authToken);
    setUser(authUser);
    localStorage.setItem(TOKEN_KEY, authToken);
    localStorage.setItem(USER_KEY, JSON.stringify(authUser));
  };

  const clearSession = () => {
    setToken(null);
    setUser(null);
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
  };

  return (
    <Routes>
      <Route path="/" element={token ? <Navigate to="/dashboard" replace /> : <Navigate to="/login" replace />} />
      <Route
        path="/login"
        element={<LoginPage token={token} saveSession={saveSession} flash={flash} setFlash={setFlash} />}
      />
      <Route
        path="/register"
        element={<RegisterPage token={token} saveSession={saveSession} flash={flash} setFlash={setFlash} />}
      />
      <Route
        path="/dashboard"
        element={
          <ProtectedRoute token={token}>
            <DashboardPage user={user} token={token} onLogout={clearSession} setFlash={setFlash} />
          </ProtectedRoute>
        }
      />
      <Route
        path="/create"
        element={
          <ProtectedRoute token={token}>
            <CreatePollPage user={user} token={token} setFlash={setFlash} />
          </ProtectedRoute>
        }
      />
      <Route path="/poll/:id" element={<PublicPollPage />} />
    </Routes>
  );
}

function ProtectedRoute({ token, children }) {
  if (!token) return <Navigate to="/login" replace />;
  return children;
}

function LoginPage({ token, saveSession, flash, setFlash }) {
  const navigate = useNavigate();
  const [form, setForm] = useState({ email: "", password: "" });
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (token) navigate("/dashboard", { replace: true });
  }, [token, navigate]);

  const handleSubmit = async () => {
    setLoading(true);
    setFlash("");

    try {
      const response = await fetch(`${API_BASE}/api/auth/login`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });

      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || "Login failed");
      }

      saveSession(data.token, data.user);
      navigate("/dashboard", { replace: true });
    } catch (error) {
      setFlash(error.message || "Login failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthLayout>
      <div className="panel auth-panel">
        <p className="eyebrow">Welcome back</p>
        <h1>Login to PulsePoll</h1>

        <label>
          Email
          <input
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            placeholder="you@example.com"
          />
        </label>

        <label>
          Password
          <input
            type="password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
            placeholder="••••••••"
          />
        </label>

        {flash && <div className="message-box error">{flash}</div>}

        <button className="primary-btn" onClick={handleSubmit} disabled={loading}>
          {loading ? "Signing in..." : "Login"}
        </button>

        <p className="auth-link-row">
          Need an account?
          <Link to="/register">Create one</Link>
        </p>
      </div>
    </AuthLayout>
  );
}

function RegisterPage({ token, saveSession, flash, setFlash }) {
  const navigate = useNavigate();
  const [form, setForm] = useState({ name: "", email: "", password: "" });
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (token) navigate("/dashboard", { replace: true });
  }, [token, navigate]);

  const handleSubmit = async () => {
    setLoading(true);
    setFlash("");

    try {
      const response = await fetch(`${API_BASE}/api/auth/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });

      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || "Registration failed");
      }

      saveSession(data.token, data.user);
      navigate("/dashboard", { replace: true });
    } catch (error) {
      setFlash(error.message || "Registration failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <AuthLayout>
      <div className="panel auth-panel">
        <p className="eyebrow">Launch your first poll</p>
        <h1>Create your account</h1>

        <label>
          Full name
          <input
            type="text"
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            placeholder="Jane Doe"
          />
        </label>

        <label>
          Email
          <input
            type="email"
            value={form.email}
            onChange={(e) => setForm({ ...form, email: e.target.value })}
            placeholder="you@example.com"
          />
        </label>

        <label>
          Password
          <input
            type="password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
            placeholder="At least 6 characters"
          />
        </label>

        {flash && <div className="message-box error">{flash}</div>}

        <button className="primary-btn" onClick={handleSubmit} disabled={loading}>
          {loading ? "Creating account..." : "Register"}
        </button>

        <p className="auth-link-row">
          Already registered?
          <Link to="/login">Login here</Link>
        </p>
      </div>
    </AuthLayout>
  );
}

function DashboardPage({ user, token, onLogout, setFlash }) {
  const [polls, setPolls] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [shareMessage, setShareMessage] = useState("");
  const navigate = useNavigate();

  const loadPolls = async () => {
    setLoading(true);
    setError("");

    try {
      const response = await fetch(`${API_BASE}/api/my-polls`, {
        headers: { Authorization: `Bearer ${token}` },
      });

      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Unable to load polls");
      setPolls(data.polls || []);
    } catch (err) {
      setError(err.message || "Unable to load polls");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadPolls();
  }, []);

  const openPoll = (pollId) => navigate(`/poll/${pollId}`);

  return (
    <div className="page-shell">
      <header className="app-header">
        <div>
          <p className="eyebrow">PulsePoll</p>
          <h2>Dashboard</h2>
        </div>
        <div className="header-actions">
          <span className="user-pill">{user?.name || "User"}</span>
          <Link to="/create" className="action-link">Create Poll</Link>
          <button className="secondary-btn" onClick={onLogout}>Logout</button>
        </div>
      </header>

      <div className="stats-grid">
        <div className="mini-card">
          <span>Total polls</span>
          <strong>{polls.length}</strong>
        </div>
        <div className="mini-card">
          <span>Realtime</span>
          <strong>Live</strong>
        </div>
        <div className="mini-card">
          <span>Account</span>
          <strong>{user?.email || "Active"}</strong>
        </div>
      </div>

      <section className="panel dashboard-panel">
        <div className="section-head">
          <h3>My Polls</h3>
          <button className="primary-btn small" onClick={() => navigate("/create")}>New Poll</button>
        </div>

        {loading && <p className="loading-text">Loading polls...</p>}
        {error && <div className="message-box error">{error}</div>}
        {shareMessage && <div className="message-box">{shareMessage}</div>}

        {!loading && polls.length === 0 && !error && (
          <div className="empty-state-box">No polls yet. Create your first public vote.</div>
        )}

        <div className="poll-list">
          {polls.map((poll) => (
            <div className="poll-row" key={poll.id}>
              <div>
                <h4>{poll.question}</h4>
                <p>{poll.options?.length || 0} options</p>
              </div>

              <div className="poll-actions">
                <button className="secondary-btn small" onClick={() => openPoll(poll.id)}>Open</button>
                <button
                  className="secondary-btn small"
                  onClick={() => {
                    const url = `${window.location.origin}/poll/${poll.id}`;
                    navigator.clipboard?.writeText(url);
                    setShareMessage("Share link copied to clipboard.");
                    setFlash("Share link copied to clipboard.");
                  }}
                >
                  Copy link
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function CreatePollPage({ user, token, setFlash }) {
  const navigate = useNavigate();
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState(["", ""]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const updateOption = (index, value) => {
    const next = [...options];
    next[index] = value;
    setOptions(next);
  };

  const addOption = () => setOptions([...options, ""]);
  const removeOption = (index) => {
    if (options.length <= 2) return;
    setOptions(options.filter((_, i) => i !== index));
  };

  const handleCreate = async () => {
    const cleanedOptions = options.map((option) => option.trim()).filter(Boolean);
    if (!question.trim()) {
      setError("Question is required.");
      return;
    }
    if (cleanedOptions.length < 2) {
      setError("At least two valid options are required.");
      return;
    }

    setLoading(true);
    setError("");

    try {
      const response = await fetch(`${API_BASE}/api/polls`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({
          question: question.trim(),
          options: cleanedOptions,
        }),
      });

      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Unable to create poll");

      const pollId = data.poll?.id;
      const shareUrl = data.shareUrl || `${window.location.origin}/poll/${pollId}`;
      setFlash(`Poll created successfully. Share link: ${shareUrl}`);
      navigate(`/poll/${pollId}`);
    } catch (err) {
      setError(err.message || "Unable to create poll");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="page-shell">
      <header className="app-header">
        <div>
          <p className="eyebrow">PulsePoll</p>
          <h2>Create Poll</h2>
        </div>
        <div className="header-actions">
          <Link to="/dashboard" className="action-link">Back to Dashboard</Link>
          <span className="user-pill">{user?.name || "User"}</span>
        </div>
      </header>

      <section className="panel form-panel">
        <label>
          Poll question
          <textarea
            rows={4}
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="Which feature should we ship next?"
          />
        </label>

        <div className="option-editor">
          {options.map((option, index) => (
            <div className="option-row" key={index}>
              <input
                type="text"
                value={option}
                onChange={(e) => updateOption(index, e.target.value)}
                placeholder={`Option ${index + 1}`}
              />
              {options.length > 2 && (
                <button className="icon-button" onClick={() => removeOption(index)} type="button">Remove</button>
              )}
            </div>
          ))}
        </div>

        <div className="button-row">
          <button className="secondary-btn" onClick={addOption} type="button">Add option</button>
          <button className="primary-btn" onClick={handleCreate} disabled={loading}>
            {loading ? "Creating poll..." : "Create poll"}
          </button>
        </div>

        {error && <div className="message-box error">{error}</div>}
      </section>
    </div>
  );
}

function PublicPollPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [poll, setPoll] = useState(null);
  const [loading, setLoading] = useState(false);
  const [voting, setVoting] = useState(false);
  const [error, setError] = useState("");
  const [voteMessage, setVoteMessage] = useState("");

  const loadPoll = async () => {
    if (!id) return;
    setLoading(true);
    setError("");

    try {
      const response = await fetch(`${API_BASE}/api/polls/${id}`);
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Poll not found");
      setPoll(data);
    } catch (err) {
      setError(err.message || "Unable to load poll");
      setPoll(null);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadPoll();
  }, [id]);

  useEffect(() => {
    if (!id) return;

    const stream = new EventSource(`https://pulsepoll-ypsp.onrender.com/api/polls/${id}/stream`);
   console.log("Creating EventSource for poll stream:", `${API_BASE}/api/polls/${id}/stream`);

    stream.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        console.log("SSE message received", payload);
        setPoll((current) => {
          if (!current) return current;
          return {
            ...current,
            votes: payload.votes || current.votes,
            question: payload.question || current.question,
            options: payload.options || current.options,
            updatedAt: payload.updatedAt || current.updatedAt,
          };
        });
      } catch (error) {
        console.error("SSE parse error", error);
      }
    };

    stream.onerror = () => {
      if (stream.readyState === EventSource.CLOSED) {
        console.log("EventSource closed for poll", id);
        stream.close();
      } else {
        console.log("EventSource connection issue for poll", id, "readyState:", stream.readyState);
      }
    };

    return () => {
      console.log("Closing EventSource for poll", id);
      stream.close();
    };
  }, [id]);

  const totalVotes = useMemo(() => {
    if (!poll?.votes) return 0;
    return poll.votes.reduce((sum, value) => sum + Number(value || 0), 0);
  }, [poll]);

  const handleVote = async (optionIndex) => {
    setVoting(true);
    setVoteMessage("");

    try {
      const response = await fetch(`${API_BASE}/api/polls/${id}/vote`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ optionIndex }),
      });

      const data = await response.json();
      if (!response.ok) throw new Error(data.error || "Vote failed");

      setPoll(data.poll);
      setVoteMessage("Vote recorded successfully.");
    } catch (err) {
      setVoteMessage(err.message || "Unable to vote");
    } finally {
      setVoting(false);
    }
  };

  if (loading) {
    return <div className="page-shell centered"><div className="panel loading-card">Loading poll...</div></div>;
  }

  if (error || !poll) {
    return (
      <div className="page-shell centered">
        <div className="panel loading-card">
          <h3>Invalid or unavailable poll</h3>
          <p>{error || "This poll could not be found."}</p>
          <button className="primary-btn" onClick={() => navigate("/login")}>Back to home</button>
        </div>
      </div>
    );
  }

  return (
    <div className="page-shell public-page-shell">
      <header className="app-header">
        <div>
          <p className="eyebrow">PulsePoll</p>
          <h2>Live Poll</h2>
        </div>
        <div className="header-actions">
          <span className="live-badge">Live</span>
          <button className="secondary-btn" onClick={() => navigate("/")}>Home</button>
        </div>
      </header>

      <section className="panel poll-panel">
        <h3>{poll.question}</h3>
        <p className="poll-meta">Created {formatTime(poll.createdAt)}</p>

        <div className="vote-list">
          {poll.options.map((option, index) => {
            const count = Number(poll.votes?.[index] || 0);
            const percent = totalVotes === 0 ? 0 : Math.round((count / totalVotes) * 100);

            return (
              <button key={index} className="vote-option" onClick={() => handleVote(index)} disabled={voting}>
                <div className="vote-topline">
                  <span>{option}</span>
                  <span>{count} votes</span>
                </div>
                <div className="progress-bar">
                  <span style={{ width: `${percent}%` }} />
                </div>
                <small>{percent}%</small>
              </button>
            );
          })}
        </div>

        {voteMessage && <div className="message-box">{voteMessage}</div>}
      </section>
    </div>
  );
}

function AuthLayout({ children }) {
  return (
    <div className="auth-shell page-shell">
      <div className="brand-panel">
        <p className="eyebrow">PulsePoll</p>
        <h1>Real-Time Live Polling Platform</h1>
        <p>Create beautiful polls, share a link, and see results update instantly for everyone.</p>
      </div>
      {children}
    </div>
  );
}

export default App;