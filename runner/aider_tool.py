"""CrewAI-инструмент: разовый запуск aider в рабочей папке агента."""
import os
import shutil
import subprocess
from typing import List, Type

from pydantic import BaseModel, Field

from crewai.tools import BaseTool

from events import emit

MAX_OUTPUT_CHARS = 8000
MAX_LOG_LINES = 200
AIDER_TIMEOUT_S = 900

# aider 0.86 при ошибках API печатает их в вывод, но выходит с кодом 0
ERROR_MARKERS = (
    "litellm.",
    "NotFoundError",
    "AuthenticationError",
    "RateLimitError",
    "APIConnectionError",
    "BadRequestError",
    "aider: error",
)


class AiderInput(BaseModel):
    prompt: str = Field(
        ...,
        description=(
            "Instruction for aider: what to change in the code of this "
            "agent's working directory. Be specific and self-contained."
        ),
    )


class AiderTool(BaseTool):
    name: str = "aider"
    description: str = (
        "Apply a code change in the agent's working directory using aider "
        "AI pair programming. Input: a precise instruction/prompt. "
        "Returns aider's output (diff summary / messages)."
    )
    args_schema: Type[BaseModel] = AiderInput

    workdir: str = ""
    agent_name: str = "aider"
    aider_bin: str = "aider"
    model: str = ""
    extra_flags: List[str] = []

    def _run(self, prompt: str) -> str:
        if not shutil.which(self.aider_bin):
            emit("error", self.agent_name, f"aider binary not found: {self.aider_bin}")
            return f"ERROR: aider binary not found ({self.aider_bin})"

        cmd = [
            self.aider_bin,
            "--yes-always",
            "--no-check-update",
            # песочница: aider не трогает файлы вне поддерева workdir
            "--subtree-only",
            # crew не должен ходить в веб — это триггерит скачивание pandoc
            "--no-detect-urls",
            "--message",
            prompt,
        ]
        if self.model:
            # без явного --model aider берёт дефолт из своего конфига
            cmd += ["--model", self.model]
        cmd += list(self.extra_flags)
        emit("thought", self.agent_name, f"aider request: {prompt[:500]}")
        emit("log", self.agent_name, f"$ aider --message ... (cwd={self.workdir})")
        try:
            p = subprocess.run(
                cmd,
                cwd=self.workdir,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                env={**os.environ, "PYTHONIOENCODING": "utf-8"},
                timeout=AIDER_TIMEOUT_S,
            )
        except subprocess.TimeoutExpired:
            emit("error", self.agent_name, f"aider timed out after {AIDER_TIMEOUT_S}s")
            return "ERROR: aider timed out"

        out = ((p.stdout or "") + "\n" + (p.stderr or "")).strip()
        lines = out.splitlines()
        for line in lines[:MAX_LOG_LINES]:
            emit("log", self.agent_name, line)
        if len(lines) > MAX_LOG_LINES:
            emit("log", self.agent_name, f"... ({len(lines) - MAX_LOG_LINES} more lines)")
        emit("status", self.agent_name, f"aider exit={p.returncode}")

        err_line = next(
            (l for l in lines if any(m in l for m in ERROR_MARKERS)),
            "",
        )
        if p.returncode != 0 or err_line:
            reason = err_line or f"exit code {p.returncode}"
            emit("error", self.agent_name, f"aider failed: {reason[:500]}")
            return f"ERROR: aider failed: {reason[:1000]}\n\n{out[:MAX_OUTPUT_CHARS]}"
        return out[:MAX_OUTPUT_CHARS] if out else "(no output)"
