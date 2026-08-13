"""
api2codex - OpenAI Responses API to Chat Completions Proxy

A lightweight proxy that converts OpenAI Responses API (/v1/responses)
requests into Chat Completions (/v1/chat/completions) and converts
the responses back. Supports both streaming and non-streaming modes,
function calling, reasoning content, and multi-turn tool conversations.

This enables tools like OpenAI Codex (which use the Responses API) to work
with any Chat Completions compatible API provider.

Vendored into provider-hub from https://github.com/talkcozy/api2codex (MIT)
with one addition: UPSTREAM_USER_AGENT forwards a custom User-Agent header
upstream (some gateways reject requests without a known client UA).
"""

import json
import logging
import os
import time
import uuid

import httpx
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, StreamingResponse

__version__ = "0.1.0"

# ── Configuration ──────────────────────────────────────────────────────────

UPSTREAM_BASE_URL = os.environ.get("UPSTREAM_BASE_URL", "")
UPSTREAM_API_KEY = os.environ.get("UPSTREAM_API_KEY", "")
UPSTREAM_USER_AGENT = os.environ.get("UPSTREAM_USER_AGENT", "")
DEFAULT_MODEL = os.environ.get("DEFAULT_MODEL", "")
HOST = os.environ.get("HOST", "0.0.0.0")
PORT = int(os.environ.get("PORT", "8000"))
DEBUG = os.environ.get("DEBUG", "").lower() in ("1", "true", "yes")

logging.basicConfig(
    level=logging.DEBUG if DEBUG else logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
log = logging.getLogger("api2codex")

app = FastAPI(title="api2codex", version=__version__)


def make_id(prefix: str = "resp") -> str:
    return f"{prefix}_{uuid.uuid4().hex[:24]}"


def upstream_headers() -> dict:
    headers = {
        "Authorization": f"Bearer {UPSTREAM_API_KEY}",
        "Content-Type": "application/json",
    }
    if UPSTREAM_USER_AGENT:
        headers["User-Agent"] = UPSTREAM_USER_AGENT
    return headers


# ── Convert Responses API input → Chat Completions messages ────────────────


def input_to_messages(body: dict) -> list:
    """Convert Responses API ``input`` to Chat Completions ``messages``.

    Handles:
    - string input → single user message
    - list input with mixed roles (user, assistant, developer/system)
    - ``function_call`` items → assistant messages with ``tool_calls``
    - ``function_call_output`` items → ``tool`` role messages
    - multi-part content (text + image)
    """
    instructions = body.get("instructions")
    inp = body.get("input", "")
    messages: list[dict] = []

    if instructions:
        messages.append({"role": "system", "content": instructions})

    if isinstance(inp, str):
        messages.append({"role": "user", "content": inp})
        return messages

    if not isinstance(inp, list):
        return messages

    # Collect consecutive function_call items into a single assistant message
    pending_tool_calls: list[dict] = []

    def flush_tool_calls():
        nonlocal pending_tool_calls
        if pending_tool_calls:
            messages.append({
                "role": "assistant",
                "content": None,
                "tool_calls": list(pending_tool_calls),
            })
            pending_tool_calls = []

    for item in inp:
        if isinstance(item, str):
            flush_tool_calls()
            messages.append({"role": "user", "content": item})
            continue

        if not isinstance(item, dict):
            continue

        item_type = item.get("type", "")

        # Responses API function_call → Chat Completions assistant tool_calls
        if item_type == "function_call":
            pending_tool_calls.append({
                "id": item.get("call_id", item.get("id", "")),
                "type": "function",
                "function": {
                    "name": item.get("name", ""),
                    "arguments": item.get("arguments", "{}"),
                },
            })
            continue

        # Responses API function_call_output → Chat Completions tool message
        if item_type == "function_call_output":
            flush_tool_calls()
            messages.append({
                "role": "tool",
                "tool_call_id": item.get("call_id", ""),
                "content": item.get("output", ""),
            })
            continue

        # Regular message (user / assistant / system / developer)
        flush_tool_calls()
        role = item.get("role", "user")
        if role == "developer":
            role = "system"

        content = item.get("content", "")
        if isinstance(content, list):
            parts = []
            for c in content:
                if isinstance(c, dict):
                    c_type = c.get("type", "")
                    if c_type == "input_text":
                        parts.append(c.get("text", ""))
                    elif c_type == "input_image":
                        parts.append("[image]")
                    else:
                        parts.append(c.get("text", str(c)))
            content = "\n".join(parts)

        messages.append({"role": role, "content": content})

    flush_tool_calls()
    return messages


def build_chat_request(body: dict) -> dict:
    """Build a Chat Completions request from a Responses API request body."""
    req: dict = {
        "model": body.get("model") or DEFAULT_MODEL,
        "messages": input_to_messages(body),
        "stream": body.get("stream", False),
    }

    if body.get("temperature") is not None:
        req["temperature"] = body["temperature"]
    if body.get("max_output_tokens") is not None:
        req["max_tokens"] = body["max_output_tokens"]
    if body.get("top_p") is not None:
        req["top_p"] = body["top_p"]

    # Convert reasoning: Responses API uses {effort, summary}, Chat Completions uses reasoning_effort
    reasoning = body.get("reasoning")
    if isinstance(reasoning, dict) and reasoning.get("effort"):
        req["reasoning_effort"] = reasoning["effort"]

    # Convert tools: Responses API uses flat format, Chat Completions uses nested
    if body.get("tools"):
        tools = []
        for t in body["tools"]:
            if t.get("type") != "function":
                continue
            if "function" in t:
                # Already in Chat Completions format
                fn = t["function"]
            else:
                # Responses API flat format
                fn = t
            tools.append({
                "type": "function",
                "function": {
                    "name": fn.get("name", ""),
                    "description": fn.get("description", ""),
                    "parameters": fn.get("parameters", {}),
                },
            })
        if tools:
            req["tools"] = tools

    return req


# ── Convert Chat Completions response → Responses API response ─────────────


def chat_response_to_responses(chat_resp: dict, model: str, resp_id: str, reasoning_effort: str | None = None) -> dict:
    """Convert a non-streaming Chat Completions response to Responses API format."""
    choice = chat_resp.get("choices", [{}])[0]
    message = choice.get("message", {})
    content_text = message.get("content", "") or ""

    output_item: dict = {
        "id": make_id("msg"),
        "type": "message",
        "role": "assistant",
        "status": "completed",
        "content": [
            {
                "type": "output_text",
                "text": content_text,
                "annotations": [],
            }
        ],
    }

    if message.get("tool_calls"):
        for tc in message["tool_calls"]:
            fn = tc.get("function", {})
            output_item["content"].append({
                "type": "function_call",
                "id": tc.get("id", make_id("call")),
                "call_id": tc.get("id", make_id("call")),
                "name": fn.get("name", ""),
                "arguments": fn.get("arguments", "{}"),
            })

    usage = chat_resp.get("usage", {})

    return {
        "id": resp_id,
        "object": "response",
        "created_at": int(time.time()),
        "status": "completed",
        "model": model,
        "output": [output_item],
        "usage": {
            "input_tokens": usage.get("prompt_tokens", 0),
            "output_tokens": usage.get("completion_tokens", 0),
            "total_tokens": usage.get("total_tokens", 0),
        },
        "parallel_tool_calls": True,
        "previous_response_id": None,
        "reasoning": {"effort": reasoning_effort or "medium", "summary": "auto"},
        "text": {"format": {"type": "text"}},
        "tools": [],
        "truncation": "disabled",
    }


# ── Streaming: Chat Completions SSE → Responses API SSE ────────────────────


async def stream_chat_to_responses(chat_req: dict, model: str, resp_id: str, reasoning_effort: str | None = None):
    """Stream Chat Completions chunks and emit Responses API SSE events.

    Handles text content, reasoning content, and function calls.
    """
    created = int(time.time())
    msg_id = make_id("msg")

    full_text = ""
    total_input = 0
    total_output = 0
    output_index = 0
    msg_closed = False

    active_tool_calls: dict[int, dict] = {}
    completed_tool_calls: list[dict] = []

    def close_msg_item():
        nonlocal msg_closed, output_index
        if msg_closed:
            return
        msg_closed = True
        yield _sse({
            "type": "response.content_part.done",
            "output_index": 0,
            "content_index": 0,
            "part": {"type": "output_text", "text": full_text, "annotations": []},
        })
        yield _sse({
            "type": "response.output_item.done",
            "output_index": 0,
            "item": {
                "id": msg_id,
                "type": "message",
                "role": "assistant",
                "status": "completed",
                "content": [{"type": "output_text", "text": full_text, "annotations": []}],
            },
        })
        output_index = 1

    # response.created / response.in_progress
    empty_response = {
        "id": resp_id,
        "object": "response",
        "created_at": created,
        "status": "in_progress",
        "model": model,
        "output": [],
        "usage": None,
    }
    yield _sse({"type": "response.created", "response": empty_response})
    yield _sse({"type": "response.in_progress", "response": empty_response})

    # output_item.added + content_part.added for the message
    yield _sse({
        "type": "response.output_item.added",
        "output_index": 0,
        "item": {"id": msg_id, "type": "message", "role": "assistant", "status": "in_progress", "content": []},
    })
    yield _sse({
        "type": "response.content_part.added",
        "output_index": 0,
        "content_index": 0,
        "part": {"type": "output_text", "text": "", "annotations": []},
    })

    async with httpx.AsyncClient(timeout=httpx.Timeout(300, connect=30)) as client:
        async with client.stream(
            "POST",
            f"{UPSTREAM_BASE_URL}/chat/completions",
            json=chat_req,
            headers=upstream_headers(),
        ) as resp:
            log.info("Upstream status: %d", resp.status_code)
            if resp.status_code != 200:
                error_body = await resp.aread()
                log.error("Upstream error: %s", error_body.decode()[:500])
                yield _sse({
                    "type": "response.failed",
                    "response": {
                        "id": resp_id,
                        "status": "failed",
                        "error": {"code": "server_error", "message": error_body.decode()[:200]},
                    },
                })
                yield "data: [DONE]\n\n"
                return

            async for line in resp.aiter_lines():
                if not line.startswith("data: "):
                    continue
                data_str = line[6:].strip()
                if data_str == "[DONE]":
                    log.debug("Stream DONE, text_len=%d tool_calls=%d", len(full_text), len(completed_tool_calls))
                    break

                try:
                    chunk = json.loads(data_str)
                except json.JSONDecodeError:
                    continue

                choices = chunk.get("choices", [])
                if not choices:
                    u = chunk.get("usage")
                    if u:
                        total_input = u.get("prompt_tokens", 0)
                        total_output = u.get("completion_tokens", 0)
                    continue

                delta = choices[0].get("delta", {})
                finish_reason = choices[0].get("finish_reason")

                # reasoning content
                reasoning = delta.get("reasoning_content", "")
                if reasoning:
                    yield _sse({
                        "type": "response.reasoning_text.delta",
                        "item_id": msg_id,
                        "output_index": 0,
                        "content_index": 0,
                        "delta": reasoning,
                    })

                # text content
                text = delta.get("content", "")
                if text:
                    full_text += text
                    yield _sse({
                        "type": "response.output_text.delta",
                        "item_id": msg_id,
                        "output_index": 0,
                        "content_index": 0,
                        "delta": text,
                    })

                # tool calls
                if delta.get("tool_calls"):
                    for tc in delta["tool_calls"]:
                        tc_index = tc.get("index", 0)
                        tc_id = tc.get("id")
                        fn = tc.get("function", {})

                        if tc_id and tc_id not in [t.get("id") for t in active_tool_calls.values()]:
                            for ev in close_msg_item():
                                yield ev

                            active_tool_calls[tc_index] = {
                                "id": tc_id,
                                "name": fn.get("name", ""),
                                "arguments": fn.get("arguments", ""),
                            }
                            log.debug("Tool call started: %s id=%s", fn.get("name"), tc_id)

                            yield _sse({
                                "type": "response.output_item.added",
                                "output_index": output_index + tc_index,
                                "item": {
                                    "id": tc_id,
                                    "type": "function_call",
                                    "call_id": tc_id,
                                    "name": fn.get("name", ""),
                                    "arguments": "",
                                    "status": "in_progress",
                                },
                            })
                        elif tc_index in active_tool_calls:
                            args_delta = fn.get("arguments", "")
                            active_tool_calls[tc_index]["arguments"] += args_delta
                            yield _sse({
                                "type": "response.function_call_arguments.delta",
                                "item_id": active_tool_calls[tc_index]["id"],
                                "output_index": output_index + tc_index,
                                "delta": args_delta,
                            })

                if finish_reason == "tool_calls":
                    for ev in close_msg_item():
                        yield ev
                    for tc_index, tc_info in sorted(active_tool_calls.items()):
                        completed_tool_calls.append(tc_info)
                        yield _sse({
                            "type": "response.function_call_arguments.done",
                            "item_id": tc_info["id"],
                            "output_index": output_index + tc_index,
                            "arguments": tc_info["arguments"],
                        })
                        yield _sse({
                            "type": "response.output_item.done",
                            "output_index": output_index + tc_index,
                            "item": {
                                "id": tc_info["id"],
                                "type": "function_call",
                                "call_id": tc_info["id"],
                                "name": tc_info["name"],
                                "arguments": tc_info["arguments"],
                                "status": "completed",
                            },
                        })
                    active_tool_calls.clear()

                u = chunk.get("usage")
                if u:
                    total_input = u.get("prompt_tokens", 0)
                    total_output = u.get("completion_tokens", 0)

    # Close text message if still open
    for ev in close_msg_item():
        yield ev

    # Complete any remaining tool calls (some models don't send finish_reason=tool_calls)
    for tc_index, tc_info in sorted(active_tool_calls.items()):
        if tc_info not in completed_tool_calls:
            completed_tool_calls.append(tc_info)
            yield _sse({
                "type": "response.function_call_arguments.done",
                "item_id": tc_info["id"],
                "output_index": output_index + tc_index,
                "arguments": tc_info["arguments"],
            })
            yield _sse({
                "type": "response.output_item.done",
                "output_index": output_index + tc_index,
                "item": {
                    "id": tc_info["id"],
                    "type": "function_call",
                    "call_id": tc_info["id"],
                    "name": tc_info["name"],
                    "arguments": tc_info["arguments"],
                    "status": "completed",
                },
            })

    # Build final output items
    output_items = []
    if full_text:
        output_items.append({
            "id": msg_id,
            "type": "message",
            "role": "assistant",
            "status": "completed",
            "content": [{"type": "output_text", "text": full_text, "annotations": []}],
        })
    for tc in completed_tool_calls:
        output_items.append({
            "id": tc["id"],
            "type": "function_call",
            "call_id": tc["id"],
            "name": tc["name"],
            "arguments": tc["arguments"],
            "status": "completed",
        })

    yield _sse({
        "type": "response.completed",
        "response": {
            "id": resp_id,
            "object": "response",
            "created_at": created,
            "status": "completed",
            "model": model,
            "output": output_items,
            "usage": {
                "input_tokens": total_input,
                "output_tokens": total_output,
                "total_tokens": total_input + total_output,
            },
            "parallel_tool_calls": True,
            "previous_response_id": None,
            "reasoning": {"effort": reasoning_effort or "medium", "summary": "auto"},
            "text": {"format": {"type": "text"}},
            "tools": [],
            "truncation": "disabled",
        },
    })

    yield "data: [DONE]\n\n"


def _sse(data: dict) -> str:
    """Format a dict as an SSE data line."""
    return f"data: {json.dumps(data)}\n\n"


# ── Endpoints ──────────────────────────────────────────────────────────────


@app.post("/v1/responses")
async def handle_responses(request: Request):
    body = await request.json()
    model = body.get("model") or DEFAULT_MODEL
    stream = body.get("stream", False)
    resp_id = make_id("resp")

    # Extract reasoning effort for passing to response conversion
    reasoning = body.get("reasoning")
    reasoning_effort = reasoning.get("effort") if isinstance(reasoning, dict) else None

    log.info(
        "Request: model=%s stream=%s input_type=%s reasoning_effort=%s",
        model, stream, type(body.get("input")).__name__, reasoning_effort,
    )

    chat_req = build_chat_request(body)
    log.debug(
        "Chat request: stream=%s msgs=%d model=%s tools=%d reasoning_effort=%s",
        chat_req.get("stream"),
        len(chat_req.get("messages", [])),
        chat_req.get("model"),
        len(chat_req.get("tools", [])),
        chat_req.get("reasoning_effort"),
    )

    if stream:
        return StreamingResponse(
            stream_chat_to_responses(chat_req, model, resp_id, reasoning_effort),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Request-Id": resp_id,
            },
        )

    async with httpx.AsyncClient(timeout=httpx.Timeout(300, connect=30)) as client:
        resp = await client.post(
            f"{UPSTREAM_BASE_URL}/chat/completions",
            json=chat_req,
            headers=upstream_headers(),
        )
        chat_resp = resp.json()

    return JSONResponse(chat_response_to_responses(chat_resp, model, resp_id, reasoning_effort))


@app.get("/v1/models")
async def list_models():
    async with httpx.AsyncClient(timeout=10) as client:
        resp = await client.get(
            f"{UPSTREAM_BASE_URL}/models",
            headers=upstream_headers(),
        )
        return JSONResponse(resp.json())


@app.get("/health")
async def health():
    return {"status": "ok", "version": __version__}


# ── Main ───────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import uvicorn

    if not UPSTREAM_BASE_URL:
        log.error("UPSTREAM_BASE_URL environment variable is required")
        raise SystemExit(1)
    if not UPSTREAM_API_KEY:
        log.error("UPSTREAM_API_KEY environment variable is required")
        raise SystemExit(1)

    log.info("Starting api2codex v%s on %s:%d", __version__, HOST, PORT)
    log.info("Upstream: %s", UPSTREAM_BASE_URL)
    uvicorn.run(app, host=HOST, port=PORT)