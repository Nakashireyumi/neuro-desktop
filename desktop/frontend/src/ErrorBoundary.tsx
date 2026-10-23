import React from "react";

type ErrorBoundaryProps = React.PropsWithChildren;

type ErrorBoundaryState = {
  hasError: boolean;
  error: unknown;
};

class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: unknown): Partial<ErrorBoundaryState> {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error("ErrorBoundary caught an error:", error, errorInfo);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div style={{ padding: "20px", background: "red", color: "white" }}>
          <h1>Something went wrong.</h1>
          <details style={{ whiteSpace: "pre-wrap" }}>
            {this.state.error ? String(this.state.error) : null}
          </details>
        </div>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
