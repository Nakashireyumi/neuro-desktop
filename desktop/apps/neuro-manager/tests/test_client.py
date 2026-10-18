import json
import os
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock

from neuro_manager.client import ApiError, Client, external_url, normalise_policy, poll_delay, server_url


class ClientTests(unittest.TestCase):
    def setUp(self):
        self.received = []
        self.operator_headers = []
        received = self.received
        operator_headers = self.operator_headers

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                received.append((self.path, self.headers.get("X-ND-Token")))
                operator_headers.append(self.headers.get("X-ND-Operator-Token"))
                if self.path.endswith("redirect"):
                    self.send_response(302)
                    self.send_header("Location", "/api/should-not-receive-token")
                    self.end_headers()
                    return
                self.send_response(401 if self.path.endswith("denied") else 200)
                self.end_headers()
                self.wfile.write(b"invalid JSON" if self.path.endswith("invalid") else json.dumps({"ok": True, "error": "invalid token"}).encode())

            def log_message(self, *_):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.client = Client(f"http://127.0.0.1:{self.server.server_port}", "memory-only-token")

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_token_is_a_header_and_never_a_url(self):
        self.assertTrue(self.client.request("/api/status")["ok"])
        self.assertEqual(self.received, [("/api/status", "memory-only-token")])

    def test_redirects_cannot_forward_the_token(self):
        with self.assertRaisesRegex(ApiError, "redirected"):
            self.client.request("/api/redirect")
        self.assertEqual(len(self.received), 1)

    def test_operator_approval_is_explicit_and_never_reused(self):
        self.client.request("/api/status")
        self.client.request("/api/status", operator_token="fresh-approval")
        self.client.request("/api/status")
        self.assertEqual(self.operator_headers, [None, "fresh-approval", None])
        self.assertEqual(self.client.token, "memory-only-token")

    def test_ambient_proxy_cannot_receive_session_or_approval_tokens(self):
        proxy = self.client.server
        with mock.patch.dict(os.environ, {"HTTP_PROXY": proxy, "http_proxy": proxy, "HTTPS_PROXY": proxy, "https_proxy": proxy, "NO_PROXY": "", "no_proxy": ""}):
            client = Client("http://127.0.0.1:1", "private-session-token")
            with self.assertRaises(ApiError):
                client.request("/api/status", operator_token="fresh-private-approval")
        self.assertEqual(self.received, [], "an ambient proxy must receive neither credential")

    def test_invalid_token_headers_never_echo_the_secret(self):
        secret = "private-fixture-secret\nsecond-line"
        with self.assertRaises(ApiError) as raised:
            Client(self.client.server, secret)
        self.assertNotIn("private-fixture-secret", str(raised.exception))
        with self.assertRaises(ApiError) as raised:
            self.client.request("/api/status", operator_token=secret)
        self.assertNotIn("private-fixture-secret", str(raised.exception))
        self.assertEqual(self.received, [])

    def test_errors_preserve_status_and_reject_malformed_or_large_responses(self):
        with self.assertRaises(ApiError) as raised:
            self.client.request("/api/denied")
        self.assertEqual(raised.exception.status, 401)
        with self.assertRaisesRegex(ApiError, "invalid JSON"):
            self.client.request("/api/invalid")
        with mock.patch("neuro_manager.client.MAX_RESPONSE_BYTES", 10):
            with self.assertRaisesRegex(ApiError, "size limit"):
                self.client.request("/api/status")


class ModelTests(unittest.TestCase):
    def test_setup_links_only_open_explicit_https_destinations(self):
        self.assertEqual(external_url("https://github.com/owner/project#setup"), "https://github.com/owner/project#setup")
        for value in (None, "file:///etc/passwd", "javascript:alert(1)", "https://user:secret@example.com", "https://example.com:99999", "http://example.com"):
            self.assertIsNone(external_url(value))

    def test_server_addresses_reject_credentials_and_ambiguous_destinations(self):
        self.assertEqual(server_url(" https://example.com/nd/ "), "https://example.com/nd")
        for value in ["file:///tmp/server", "http://user:secret@example.com", "https://example.com/?token=secret", "https://example.com/#token", "http://localhost:99999"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                server_url(value)

    def test_policy_edits_preserve_metadata_and_accept_boolean_scopes(self):
        original = {"metadata": {"owner": "operator"}, "scopes": {"input": True, "shell": False, "custom": {"allowed": False}}}
        policy = normalise_policy(original)
        self.assertTrue(policy["scopes"]["input"]["allowed"])
        self.assertFalse(policy["scopes"]["shell"]["allowed"])
        self.assertEqual(policy["metadata"], original["metadata"])
        policy["scopes"]["input"]["allowed"] = False
        self.assertIs(original["scopes"]["input"], True)
        self.assertIn("custom", policy["scopes"])

    def test_idle_and_failed_connections_back_off(self):
        self.assertEqual(poll_delay(True), 5)
        self.assertEqual(poll_delay(False), 30)
        self.assertEqual(poll_delay(True, 1), 10)
        self.assertEqual(poll_delay(False, 20), 60)

    def test_policy_shorthand_does_not_turn_false_strings_on(self):
        policy = normalise_policy({"default_allow": "no", "scopes": {"input": {"allowed": "false", "requestable": "yes", "limits": None}, "shell": None}})
        self.assertFalse(policy["default_allow"])
        self.assertFalse(policy["scopes"]["input"]["allowed"])
        self.assertTrue(policy["scopes"]["input"]["requestable"])
        self.assertFalse(policy["scopes"]["shell"]["allowed"])
        with self.assertRaises(ApiError):
            normalise_policy({"scopes": {"input": "allow everything"}})
