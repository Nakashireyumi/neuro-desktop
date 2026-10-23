"""neuro-client: the program that runs on the PC Neuro controls.

It is the controller agent, packaged as one executable (see scripts/build-client.sh).
It holds no policy, no dashboard, and no vision. The server decides what happens,
and this program carries out the commands it receives over the executor connection.
"""

import sys

from controller.agent import main

if __name__ == "__main__":
    sys.exit(main())
