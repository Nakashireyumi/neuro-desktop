"""Wall-clock bounds for socket I/O, including peers that trickle bytes."""

import socket
import threading
import time


class SocketDeadline:
    """Interrupt a socket at an absolute monotonic deadline.

    Socket timeouts alone restart after each successful read. Shutdown wakes
    buffered header/body reads too; the caller still owns and closes the socket.
    Each admitted HTTP connection uses at most one of these bounded timers.
    """

    def __init__(self, connection, deadline, how=socket.SHUT_RDWR):
        self.connection = connection
        self.how = how
        self.expired = threading.Event()
        self.timer = threading.Timer(max(0.0, deadline - time.monotonic()), self._expire)
        self.timer.daemon = True

    def _expire(self):
        self.expired.set()
        try:
            self.connection.shutdown(self.how)
        except OSError:
            pass

    def __enter__(self):
        self.timer.start()
        return self

    def cancel(self):
        self.timer.cancel()
        # Do not let an old timer interrupt a later request on a reused socket.
        self.timer.join()

    def __exit__(self, *exc):
        self.cancel()
