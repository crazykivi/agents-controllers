"""Crew-runner: читает JSON-спеку задачи из stdin, оркестрирует агентов через crewai,
пишет JSONL-события в stdout для Go-бэкенда.

Режимы:
  sequential — агенты работают цепочкой, следующий видит отчёт предыдущего;
  parallel   — координатор пишет общий план (AGENTS_PLAN.md в shared_dir),
               затем каждый агент делает свою секцию плана и складывает
               отчёт в status/<имя>.md.
"""
import json
import os
import sys
import threading
import warnings

warnings.filterwarnings("ignore")

from events import emit  # noqa: E402

# Провайдер -> переменная окружения с ключом (crewai требует именно её).
PROVIDER_KEY_ENV = {
    "openrouter": "OPENROUTER_API_KEY",
    "anthropic": "ANTHROPIC_API_KEY",
    "gemini": "GEMINI_API_KEY",
    "google": "GEMINI_API_KEY",
}


def read_spec() -> dict:
    line = sys.stdin.readline()
    if not line.strip():
        raise RuntimeError("empty spec on stdin")
    return json.loads(line)


def wait_decision() -> str:
    """Читает решение по плану из stdin: approve | reject."""
    line = sys.stdin.readline()
    return line.strip().lower()


def make_llm(model: str):
    """LLM с диагностикой вместо трейсбека pydantic при ошибке конфигурации."""
    from crewai import LLM

    provider = model.split("/", 1)[0] if "/" in model else ""
    kwargs = {}
    key_env = PROVIDER_KEY_ENV.get(provider)
    if key_env and not os.environ.get(key_env):
        # aider-конвенция: ключ openrouter может лежать в OPENAI_API_KEY
        fallback = os.environ.get("OPENAI_API_KEY")
        if fallback:
            kwargs["api_key"] = fallback
        else:
            raise RuntimeError(
                f"model {model!r}: set {key_env} (or OPENAI_API_KEY) in the "
                "backend environment / backend/.env"
            )
    try:
        return LLM(model=model, **kwargs)
    except Exception as e:
        raise RuntimeError(f"model {model!r}: {str(e).splitlines()[0]}") from e


def register_event_hooks() -> None:
    try:
        from crewai.events import crewai_event_bus
        from crewai.events.types.agent_events import (
            AgentExecutionCompletedEvent,
            AgentExecutionErrorEvent,
            AgentExecutionStartedEvent,
        )
        from crewai.events.types.llm_events import LLMThinkingChunkEvent
        from crewai.events.types.tool_usage_events import (
            ToolUsageFinishedEvent,
            ToolUsageStartedEvent,
        )
    except Exception as e:  # версия crewai не совпала — работаем без тонких хуков
        emit("log", "crew", f"event hooks unavailable: {e}")
        return

    def role(ev) -> str:
        return getattr(ev, "agent_role", None) or "crew"

    @crewai_event_bus.on(AgentExecutionStartedEvent)
    def _started(source, ev):
        emit("status", role(ev), f"started: {getattr(ev, 'task_name', '') or ''}")

    @crewai_event_bus.on(AgentExecutionCompletedEvent)
    def _completed(source, ev):
        out = getattr(ev, "output", None)
        text = getattr(out, "raw", None) or str(out)
        emit("thought", role(ev), f"agent output: {text[:2000]}")

    @crewai_event_bus.on(AgentExecutionErrorEvent)
    def _error(source, ev):
        emit("error", role(ev), str(getattr(ev, "error", ev))[:2000])

    @crewai_event_bus.on(LLMThinkingChunkEvent)
    def _thinking(source, ev):
        chunk = getattr(ev, "chunk", "")
        if chunk:
            emit("thought", role(ev), str(chunk)[:2000])

    @crewai_event_bus.on(ToolUsageStartedEvent)
    def _tool_started(source, ev):
        emit(
            "log",
            role(ev),
            f"tool {getattr(ev, 'tool_name', '?')} args: {str(getattr(ev, 'tool_args', ''))[:500]}",
        )

    @crewai_event_bus.on(ToolUsageFinishedEvent)
    def _tool_finished(source, ev):
        emit("log", role(ev), f"tool {getattr(ev, 'tool_name', '?')} finished")


def build_agent(a: dict, aider_bin: str):
    """crewai-Agent с aider-инструментом в рабочей папке агента."""
    from crewai import Agent

    from aider_tool import AiderTool

    tool = AiderTool(
        workdir=a["workdir"],
        agent_name=a["name"],
        aider_bin=aider_bin,
        model=a.get("model") or "",
        extra_flags=a.get("aider_flags") or [],
    )
    llm = make_llm(a["model"]) if a.get("model") else None
    return Agent(
        role=a.get("role") or f"{a['name']} developer",
        goal=a.get("goal") or "Complete the assigned part of the task using aider.",
        backstory=a.get("backstory")
        or "A pragmatic engineer who edits code only via the aider tool.",
        tools=[tool],
        llm=llm,
        verbose=False,
        allow_delegation=False,
    )


def first_llm(spec: dict):
    for a in spec["agents"]:
        if a.get("model"):
            return make_llm(a["model"])
    return None


# ---------------- sequential ----------------

def run_sequential(spec: dict) -> str:
    from crewai import Crew, Process, Task

    agents = []
    tasks = []
    for a in spec["agents"]:
        agent = build_agent(a, spec.get("aider_bin", "aider"))
        agents.append(agent)
        tasks.append(
            Task(
                description=(
                    f"TASK: {spec['task']['title']}\n\n"
                    f"{spec['task']['description']}\n\n"
                    f"YOUR RESPONSIBILITY (as {agent.role}): {agent.goal}\n"
                    f"Work only inside your working directory: {a['workdir']}. "
                    "Use the aider tool for all code changes and verify the result."
                ),
                expected_output=(
                    "Short report: what was changed in the repo and how it was verified."
                ),
                agent=agent,
            )
        )
    crew = Crew(agents=agents, tasks=tasks, process=Process.sequential, verbose=False)
    result = crew.kickoff()
    return getattr(result, "raw", None) or str(result)


# ---------------- parallel ----------------

def generate_plan(spec: dict, shared_dir: str) -> str:
    """Один прогон координатора: дробит задачу на независимые секции по агентам."""
    from crewai import Agent, Crew, Process, Task

    planner = Agent(
        role="Coordinator",
        goal="Split the task into independent parts, one per executor.",
        backstory="Experienced tech lead who writes crisp, non-overlapping work plans.",
        llm=first_llm(spec),
        verbose=False,
        allow_delegation=False,
    )
    roster = "\n".join(
        f"- {a['name']} (role: {a.get('role') or a['name']}): works in {a['workdir']}"
        for a in spec["agents"]
    )
    task = Task(
        description=(
            f"TASK: {spec['task']['title']}\n\n"
            f"{spec['task']['description']}\n\n"
            f"EXECUTORS (they will work IN PARALLEL, independently):\n{roster}\n\n"
            "Write an execution plan in markdown:\n"
            "1. '## Goal' — 2-3 sentences of shared understanding.\n"
            "2. One '## <executor name>' section per executor with 2-6 checklist "
            "items (- [ ] ...). Sections MUST be independent: no executor edits "
            "another's files or waits for another's output."
        ),
        expected_output="Markdown plan with a section per executor and checklist items.",
        agent=planner,
    )
    crew = Crew(agents=[planner], tasks=[task], process=Process.sequential, verbose=False)
    result = crew.kickoff()
    plan = (getattr(result, "raw", None) or str(result)).strip()

    plan_path = os.path.join(shared_dir, "AGENTS_PLAN.md")
    with open(plan_path, "w", encoding="utf-8") as f:
        f.write(f"# Plan: {spec['task']['title']}\n\n{plan}\n")
    emit("plan", "crew", f"plan written to {plan_path}\n\n{plan[:6000]}")
    return plan


def run_agent_parallel(a: dict, spec: dict, plan: str, shared_dir: str, results: dict) -> None:
    from crewai import Crew, Process, Task

    name = a["name"]
    try:
        agent = build_agent(a, spec.get("aider_bin", "aider"))
        task = Task(
            description=(
                f"TASK: {spec['task']['title']}\n\n"
                f"{spec['task']['description']}\n\n"
                f"SHARED PLAN (context, read-only):\n{plan}\n\n"
                f"YOUR SECTION: complete the checklist from the '## {name}' section only.\n"
                f"Work ONLY inside your working directory: {a['workdir']} — "
                "other agents run in parallel in their own directories, never touch theirs. "
                "Use the aider tool for all code changes and verify each item."
            ),
            expected_output=(
                "Checklist report of your plan section: mark each item [x] done "
                "(with how it was verified) or [ ] not done (with reason)."
            ),
            agent=agent,
        )
        crew = Crew(agents=[agent], tasks=[task], process=Process.sequential, verbose=False)
        result = crew.kickoff()
        report = getattr(result, "raw", None) or str(result)
    except Exception as e:
        report = f"FAILED: {e}"
        emit("error", name, str(e)[:2000])

    status_dir = os.path.join(shared_dir, "status")
    os.makedirs(status_dir, exist_ok=True)
    status_path = os.path.join(status_dir, f"{name}.md")
    try:
        with open(status_path, "w", encoding="utf-8") as f:
            f.write(f"# {name} — report\n\n{report}\n")
        emit("status", name, f"report written to {status_path}")
    except OSError as e:
        emit("error", name, f"cannot write status file: {e}")
    results[name] = report


def run_parallel(spec: dict) -> str:
    agents = spec["agents"]
    shared_dir = spec.get("shared_dir") or agents[0]["workdir"]
    plan = generate_plan(spec, shared_dir)

    # dry-run: координатор написал план — ждём подтверждения человека
    if spec["task"].get("confirm_plan"):
        emit("status", "crew", "план готов, ожидает подтверждения (dry-run)")
        decision = wait_decision()
        if decision != "approve":
            emit("status", "crew", f"план отклонён ({decision or 'нет ответа'}) — задача остановлена")
            sys.exit(3)
        emit("status", "crew", "план подтверждён — запускаю агентов")

    results: dict = {}
    threads = []
    for a in agents:
        t = threading.Thread(
            target=run_agent_parallel, args=(a, spec, plan, shared_dir, results)
        )
        t.start()
        threads.append(t)
        emit("status", a["name"], "launched in parallel")
    for t in threads:
        t.join()

    parts = [f"# Parallel task report: {spec['task']['title']}\n"]
    for a in agents:
        parts.append(f"\n## {a['name']}\n\n{results.get(a['name'], 'NO REPORT')}\n")
    return "".join(parts)


# ---------------- entry ----------------

def main() -> None:
    spec = read_spec()
    mode = spec.get("mode") or "sequential"
    emit("status", "crew", f"task loaded: {spec['task']['title']} (mode={mode})")
    register_event_hooks()
    if mode == "parallel":
        text = run_parallel(spec)
    else:
        text = run_sequential(spec)
    emit("result", "crew", str(text)[:20000])


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        import traceback

        emit("error", "crew", str(e))
        emit("log", "crew", traceback.format_exc()[-2000:])
        sys.exit(1)
