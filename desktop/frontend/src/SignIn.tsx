import { useState, type FormEvent } from "react";
import { ApiError, api, setAdminToken } from "./api";

type Props = {
  /**
   * "signin": the server is set up and needs the dashboard token.
   * "setup": the server has no dashboard token yet, so the operator runs setup first.
   */
  mode: "signin" | "setup";
  /** The server's own explanation, when it gave one (for example, a token mismatch). */
  reason?: string;
  /** Called after the token has been checked; the app reloads its data. */
  onSignedIn: () => void;
};

/**
 * The sign-in gate. Nothing else in the dashboard is shown until the server has
 * accepted the token, so a stranger on the network sees only this page.
 */
export default function SignIn({ mode, reason, onSignedIn }: Props) {
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function signIn(event: FormEvent) {
    event.preventDefault();
    const candidate = token.trim();
    if (!candidate) {
      return;
    }
    setBusy(true);
    setError(null);
    // Store the candidate first so the session check sends it, then forget it
    // again if the server refuses it.
    setAdminToken(candidate);
    try {
      await api.session();
      setToken("");
      onSignedIn();
    } catch (failure) {
      setAdminToken("");
      if (failure instanceof ApiError && failure.status === 401) {
        setError("That token is not accepted. Use the token printed by `neuro-integration setup`.");
      } else if (failure instanceof ApiError) {
        setError(failure.message);
      } else {
        setError("Could not reach the bridge.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="signin-root">
      <section className="signin-card" aria-labelledby="signin-title">
        <h1 id="signin-title">Neuro Desktop</h1>
        {mode === "setup" ? (
          <>
            <h2>Set up the server first</h2>
            <p>
              The server has no dashboard token yet. On the machine that runs the server, run
              <code> neuro-integration setup</code>. It prints a dashboard token once. Then press
              “Check again” and sign in with it.
            </p>
          </>
        ) : (
          <>
            <h2>Sign in</h2>
            <p>
              Enter the dashboard token from <code>neuro-integration setup</code>. It is kept for this
              browser session only.
            </p>
          </>
        )}
        {reason && <p className="signin-error">{reason}</p>}
        {mode === "signin" && (
          <form className="signin-form" onSubmit={signIn}>
            <input
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="dashboard token"
              aria-label="Dashboard token"
            />
            <button type="submit" className="primary" disabled={busy || !token.trim()}>
              {busy ? "Checking…" : "Sign in"}
            </button>
          </form>
        )}
        {mode === "setup" && (
          <button type="button" className="secondary" onClick={onSignedIn}>
            Check again
          </button>
        )}
        {error && <p className="signin-error">{error}</p>}
      </section>
    </div>
  );
}
