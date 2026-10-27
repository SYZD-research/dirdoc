package analyze

import "testing"

const pythonSource = `#!/usr/bin/env python3
"""Worker pool."""
from __future__ import annotations

import os

MAX_AGENTS: int = 8
DEFAULT_NAME = "pool"
counter, total = 0, 0
TABLE = {
    "a": 1,
}
Vector = list[float]
type Point = tuple[float, float]

if os.name == "nt":
    SEP = "\\"
else:
    SEP = "/"


@dataclass(frozen=True)
class Pool(Base, metaclass=Meta):
    """A pool."""

    size: int
    limit: int = 4
    workers = []

    def __init__(self, size: int) -> None:
        self.size = size
        def helper():
            pass

    @property
    def count(self) -> int:
        return len(self.workers)

    async def spawn(
        self,
        n: int,  # how many
    ) -> list[Worker]:
        text = """
def not_a_function():
"""
        return []

    class Worker:
        name = "w"

        def run(self): ...


def main(argv=None):
    x = 1
    return 0

async def fetch(url: str) -> bytes: return b""

try:
    import ujson as json
except ImportError:
    json = None

lambda_ = lambda x: x

if __name__ == "__main__":
    args = main()
(A, B) = 1, 2
[C, D,] = 3, 4
try: import ujson
except ImportError: ujson = None
E = 1; F = 2
class Inline: x = 1; y = 2
if __name__ == "__main__": RUN = True
`

func TestPythonSymbols(t *testing.T) {
	checkSymbols(t, "pool.py", pythonSource, []string{
		"variable MAX_AGENTS 7: MAX_AGENTS: int = 8",
		`variable DEFAULT_NAME 8: DEFAULT_NAME = "pool"`,
		"variable counter 9: counter, total = 0, 0",
		"variable total 9: counter, total = 0, 0",
		`variable TABLE 10: TABLE = { "a": 1 }`,
		"variable Vector 13: Vector = list[float]",
		"type Point 14: type Point = tuple[float, float]",
		`variable SEP 17: SEP = "\\"`,
		"type Pool 23: class Pool(Base, metaclass=Meta)",
		"variable Pool.limit 27: limit: int = 4",
		"variable Pool.workers 28: workers = []",
		"function Pool.__init__ 30: def __init__(self, size: int) -> None",
		"function Pool.count 36: def count(self) -> int",
		"function Pool.spawn 39: async def spawn(self, n: int) -> list[Worker]",
		"type Pool.Worker 48: class Worker",
		`variable Pool.Worker.name 49: name = "w"`,
		"function Pool.Worker.run 51: def run(self)",
		"function main 54: def main(argv=None)",
		"function fetch 58: async def fetch(url: str) -> bytes",
		"variable json 63: json = None",
		"function lambda_ 65: lambda_ = lambda x: x",
		"variable A 69: (A, B) = 1, 2",
		"variable B 69: (A, B) = 1, 2",
		"variable C 70: [C, D] = 3, 4",
		"variable D 70: [C, D] = 3, 4",
		"variable ujson 72: ujson = None",
		"variable E 73: E = 1",
		"variable F 73: F = 2",
		"type Inline 74: class Inline",
		"variable Inline.x 74: x = 1",
		"variable Inline.y 74: y = 2",
	})
}
