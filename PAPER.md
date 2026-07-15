# 📄 **AOS: A Lightweight Semantic Orchestration System Based on Autonomous Agents and LLM-Assisted Reasoning**

*(Whitepaper — Version 1.0)*

---

## **Abstract**

AOS (Agentic Orchestration System) is a lightweight orchestration framework that enables natural-language control of deterministic workflows.
Instead of relying on Large Language Models (LLMs) to execute business logic directly, AOS delegates LLMs only for *semantic interpretation and summarization*, while actual logic execution is handled by domain-specific agents and declarative pipelines.

This architecture provides transparency, determinism, safety, and extensibility—key virtues typically missing in fully-LLM-driven “agentic” systems.
AOS allows any API or service to be exposed through a natural-language interface while preserving strict control over the underlying actions.

---

## **1. Introduction**

As organizations adopt LLMs for automation and decision support, one recurring challenge emerges: **LLMs are powerful at interpretation but unreliable at execution**. They hallucinate, lack determinism, and are not auditable.

Existing agentic frameworks often overburden LLMs with tasks they are not suited for, turning them into monolithic planners, executors, and validators. The result is:

* non-reproducible workflows
* unpredictable side effects
* unacceptable risk profiles in domains like finance, operations, DevOps or CRM
* difficulty in testing and debugging

AOS proposes a different approach:
**LLMs interpret, humans request; agents execute.**

---

## **2. Conceptual Overview**

AOS is based on three pillars:

### **2.1 Natural-Language Interface**

Users interact with the system through text or voice:

> “¿Puedo enviar 20€ a Laura?”
> “Reinicia el servicio de facturación.”
> “Créame un ticket si el cliente no tiene contrato.”

AOS interprets the intent, retrieves relevant data, and performs actions.

### **2.2 Semantic Planning via LLM**

LLMs are used only for:

* intent detection
* slot extraction
* summarization

They are *not* used for logic, conditions, branching, or actual API calls.

### **2.3 Deterministic Execution via Agents**

All workflows are executed by specialized agents coordinated through a message bus:

* APIAgent
* PlannerAgent
* InspectorAgent
* VerifierAgent
* AnalystAgent

Each agent is small, single-purpose, and testable.

### **2.4 Execution Modes (Linear vs DAG)**

AOS pipelines can be executed in two modes:
1. **Linear**: Steps are executed sequentially.
2. **DAG (Directed Acyclic Graph)**: Steps with `depends_on` relationships are executed concurrently, allowing complex orchestrations like mapping/reducing or parallel asynchronous tool execution.

### **2.5 Active Resiliency**

Instead of failing silently on API errors, the Planner acts as an active resiliency layer. If an unexpected error occurs or data is missing, it can fall back to alternative tools or emit a Clarification Request to the user (Human-in-the-Loop), asking for missing context before retrying.

---

## **3. System Architecture**

### **3.1 Runtime**

AOS runtime unifies system-wide resources:

* configuration
* LLM client
* message bus
* tool registry
* UI store
* structured logger
* readiness state

It acts as the dependency container for agents and handlers.

### **3.2 Message Bus**

A lightweight Pub/Sub mechanism that decouples agents.
Agents communicate exclusively via events, never direct calls.

This yields:

* low coupling
* concurrency-friendly design
* easy supervision
* observable message flow

### **3.3 Agents**

Each agent implements the interface:

```go
type Agent interface {
    Name() string
    Inbox() chan bus.Message
    Start()
}
```

Agents run in goroutines and react to messages.

Responsibilities:

| Agent     | Role                                      |
| --------- | ----------------------------------------- |
| APIAgent  | Entry point from HTTP/UI                  |
| Inspector | Input sanity checking                     |
| Planner   | Intent interpretation, pipeline selection |
| Verifier  | Preconditions, validations                |
| Analyst   | Summaries, NL output                      |

### **3.4 Tools**

Tools are declarative HTTP integrations described in YAML:

```yaml
tools:
  - name: banking.get_balance
    method: GET
    url: http://mock/balance
```

They represent *controlled capabilities* exposed by the system.

### **3.5 Pipelines**

Pipelines define deterministic workflows:

```yaml
pipeline:
  name: banking_transfer
  steps:
    - id: verify
      tool: banking.get_balance
      next:
        if: "$.balance > 20"
        then: transfer
        else: deny
```

---

## **4. AOS Workflow Execution Model**

### **4.1 User Request**

A user expresses a request in natural language.

### **4.2 LLM Intent Detection**

LLM parses:

* intent ("transfer check")
* entities ("Laura", "20€")
* missing data
* context

### **4.3 Pipeline Selection**

Planner maps intent → pipeline YAML.

### **4.4 Agent Coordination**

Each step emits events through the bus:

```
api → inspector → planner → verifier → tools → analyst
```

### **4.5 Tool Execution**

A step resolves into one or more tool calls (HTTP or CLI adapters).

### **4.6 Summary**

LLM summarization produces a human-readable output.

Example:

> “Puedes transferir 20€. Tu saldo actual es 157€.”

---

## **5. Design Principles**

### **5.1 LLMs for interpretation, not execution**

AOS maintains deterministic control over critical logic.

### **5.2 Message-Driven Architecture**

Agents operate asynchronously but reliably, allowing:

* parallelism
* scaling per-agent
* graceful degradation

### **5.3 Declarative Workflows**

YAML pipelines make flows:

* testable
* auditable
* reproducible
* safe

### **5.4 Extensibility**

Adding a new domain (banking, CRM, DevOps, IoT) simply requires:

* tools YAML
* pipeline YAML
* optional agent specialization

---

## **6. Comparison with Existing Systems**

| System         | Nature                 | Execution                      | Determinism | Risk |
| -------------- | ---------------------- | ------------------------------ | ----------- | ---- |
| AutoGPT, ReAct | LLM-driven agentic     | LLM decides everything         | Low         | High |
| BPMN / Camunda | Workflow engine        | Rules, scripts                 | High        | Low  |
| AOS            | Hybrid semantic engine | LLM interprets, agents execute | High        | Low  |

AOS sits in a category of its own: a **semantic orchestration layer**.

---

## **7. Use Cases**

### **Banking**

* balance inquiries
* payment workflows
* fraud checks
* reconciliation steps

### **CRM**

* ticket creation
* customer lookup
* lead qualification

### **DevOps**

* service status checks
* restarts
* incident summaries

### **IoT**

* device states
* sensor queries
* control actions

### **Enterprise Automation**

Speech-in → AOS → APIs → Speech-out

---

## **8. Safety Considerations**

Because execution is controlled by:

* agents
* pipelines
* conditions
* explicit tools

…AOS is substantially safer than LLM-executor agents.

LLMs can suggest; they cannot act without permission.

---

## **9. Implementation Summary (Go)**

* pure Go, no frameworks
* structured logging (slog + JSON)
* internal/app as composition root
* runtime DI container
* goroutine-based agents
* HTTP integration tools
* YAML configuration

---

## **10. Roadmap**

* visual pipeline editor
* multi-model LLM support
* agent supervision strategies
* distributed bus
* multi-agent debugging UI
* secure capability policies
* plugin marketplace

---

## **Conclusion**

AOS introduces a novel hybrid model for workflow automation:
LLM-assisted understanding + deterministic execution.

It is simple, safe, extensible, and suitable for real-world domains where semantic interfaces are desirable but uncontrolled AI execution is unacceptable.

AOS demonstrates that natural language can be a first-class interface to complex systems without sacrificing reliability or engineering discipline.

---

## **License**

Apache 2.0
