"""A real A2A agent (official Python SDK 1.x), JSON-RPC binding, speaking A2A 1.0 and 0.3
(by the A2A-Version header) on one endpoint.

    python a2a_server.py [port]      # default 18120, endpoint /a2a, card at /.well-known/agent-card.json

Any message starts a task that works for half a second and completes with an artifact; a
message containing "needs input" stops in input-required instead.
"""
import asyncio
import sys

import uvicorn
from starlette.applications import Starlette

from a2a.helpers import new_task_from_user_message, new_text_message, new_text_part
from a2a.server.agent_execution import AgentExecutor, RequestContext
from a2a.server.events import EventQueue
from a2a.server.request_handlers import DefaultRequestHandler
from a2a.server.routes.agent_card_routes import create_agent_card_routes
from a2a.server.routes.jsonrpc_routes import create_jsonrpc_routes
from a2a.server.tasks import InMemoryTaskStore, TaskUpdater
from a2a.types import a2a_pb2 as pb

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18120


class ComplianceReviewer(AgentExecutor):
    async def execute(self, context: RequestContext, event_queue: EventQueue) -> None:
        task = context.current_task or new_task_from_user_message(context.message)
        await event_queue.enqueue_event(task)
        updater = TaskUpdater(event_queue, task.id, task.context_id)
        text = context.get_user_input()
        if "needs input" in text:
            await updater.requires_input(new_text_message("Which jurisdiction applies?"))
            return
        await updater.start_work()
        await asyncio.sleep(0.5)
        await updater.add_artifact([new_text_part(f"Compliance review approved: {text}")], name="review")
        await updater.complete()

    async def cancel(self, context: RequestContext, event_queue: EventQueue) -> None:
        raise NotImplementedError


card = pb.AgentCard(
    name="Interop compliance reviewer",
    description="Reviews a request and approves it, for AOS's interop run.",
    version="1.0.0",
    supported_interfaces=[pb.AgentInterface(url=f"http://127.0.0.1:{PORT}/a2a", protocol_binding="JSONRPC", protocol_version="1.0")],
    capabilities=pb.AgentCapabilities(streaming=False),
    default_input_modes=["text/plain"],
    default_output_modes=["text/plain"],
    skills=[pb.AgentSkill(id="review", name="Compliance review", description="Approves a request", tags=["compliance"])],
)
handler = DefaultRequestHandler(agent_executor=ComplianceReviewer(), task_store=InMemoryTaskStore(), agent_card=card)
app = Starlette(routes=create_agent_card_routes(card) + create_jsonrpc_routes(handler, "/a2a", enable_v0_3_compat=True))

if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=PORT, log_level="warning")
