"""JSONL-протокол событий runner -> Go backend (stdout)."""
import json
import sys
import threading

_lock = threading.Lock()


def emit(kind: str, agent: str, text: str) -> None:
    """Одна строка JSON в stdout; Go раскладывает их по потоку задачи.
    Потоки параллельных агентов пишут через один замок — строки не рвутся."""
    line = json.dumps(
        {"kind": kind, "agent": agent, "text": str(text)},
        ensure_ascii=False,
    )
    with _lock:
        sys.stdout.write(line + "\n")
        sys.stdout.flush()
