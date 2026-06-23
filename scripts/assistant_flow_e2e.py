#!/usr/bin/env python3
"""Local assistant-flow smoke harness for the WeChat UI bridge route.

This script is intentionally local-only. It starts a mock OpenClaw endpoint and
a mock WeChat protocol bridge, injects one sync-message callback into a running
main service, and waits for the main service to POST a reply to /api/Msg/SendTxt.
"""

from __future__ import annotations

import argparse
import json
import os
import queue
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any


def post_json(url: str, payload: dict[str, Any], timeout: float) -> tuple[int, str]:
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace")
    except urllib.error.URLError as exc:
        return 0, str(exc.reason)


def get_json(url: str, timeout: float) -> tuple[int, str]:
    req = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", errors="replace")
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read().decode("utf-8", errors="replace")
    except urllib.error.URLError as exc:
        return 0, str(exc.reason)


def read_json_body(handler: BaseHTTPRequestHandler) -> dict[str, Any]:
    length = int(handler.headers.get("Content-Length", "0") or "0")
    raw = handler.rfile.read(length) if length else b"{}"
    if not raw:
        return {}
    return json.loads(raw.decode("utf-8"))


def write_json(handler: BaseHTTPRequestHandler, status: int, payload: dict[str, Any]) -> None:
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def sk_string(value: str) -> dict[str, str]:
    return {"string": value}


def build_sync_callback(
    *,
    wechat_id: str,
    from_wxid: str,
    to_wxid: str,
    content: str,
    sender_wxid: str,
    at_wxid: str,
) -> dict[str, Any]:
    now = int(time.time())
    msg_id = int(time.time() * 1000)
    is_group = from_wxid.endswith("@chatroom")
    sync_content = f"{sender_wxid}:\n{content}" if is_group and sender_wxid else content
    msg_source = ""
    if at_wxid:
        msg_source = f"<msgsource><atuserlist>{at_wxid}</atuserlist></msgsource>"
    return {
        "Success": True,
        "Code": 0,
        "Message": "ok",
        "Data": {
            "AddMsgs": [
                {
                    "MsgId": msg_id,
                    "NewMsgId": msg_id,
                    "FromUserName": sk_string(from_wxid),
                    "ToUserName": sk_string(to_wxid or wechat_id),
                    "Content": sk_string(sync_content),
                    "CreateTime": now,
                    "MsgType": 1,
                    "Status": 3,
                    "PushContent": content,
                    "MsgSource": msg_source,
                }
            ],
            "Status": 1,
            "Time": now,
            "Remarks": f"assistant-flow-e2e injected for {wechat_id}",
        },
    }


class Recorder:
    def __init__(self) -> None:
        self.openclaw_requests: list[dict[str, Any]] = []
        self.sent_messages: "queue.Queue[dict[str, Any]]" = queue.Queue()
        self.lock = threading.Lock()


def make_openclaw_handler(recorder: Recorder, reply: str) -> type[BaseHTTPRequestHandler]:
    class OpenClawHandler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:  # noqa: N802
            body = read_json_body(self)
            with recorder.lock:
                recorder.openclaw_requests.append(body)
            write_json(self, 200, {"reply": reply})

        def log_message(self, fmt: str, *args: Any) -> None:
            return

    return OpenClawHandler


def make_wechat_handler(recorder: Recorder) -> type[BaseHTTPRequestHandler]:
    class WeChatHandler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:  # noqa: N802
            body = read_json_body(self)
            if self.path.endswith("/Msg/SendTxt"):
                recorder.sent_messages.put(body)
                print(json.dumps({"mock_wechat_send": body}, ensure_ascii=False), flush=True)
                now = int(time.time())
                write_json(
                    self,
                    200,
                    {
                        "Success": True,
                        "Code": 0,
                        "Message": "ok",
                        "Data": {
                            "ret": 0,
                            "List": [
                                {
                                    "Ret": 0,
                                    "MsgId": now,
                                    "NewMsgId": now,
                                    "ClientMsgid": now,
                                    "Createtime": now,
                                    "Type": 1,
                                }
                            ],
                            "Count": 1,
                        },
                    },
                )
                return
            if self.path.endswith("/Login/GetCacheInfo"):
                write_json(
                    self,
                    200,
                    {
                        "Success": True,
                        "Code": 0,
                        "Message": "ok",
                        "Data": {
                            "Wxid": self.server.wechat_id,  # type: ignore[attr-defined]
                            "wxid": self.server.wechat_id,  # type: ignore[attr-defined]
                            "DeviceName": "assistant-flow-e2e",
                            "deviceName": "assistant-flow-e2e",
                        },
                    },
                )
                return
            if self.path.endswith("/User/GetContractProfile"):
                write_json(
                    self,
                    200,
                    {
                        "Success": True,
                        "Code": 0,
                        "Message": "ok",
                        "Data": {"baseResponse": {"ret": 0}, "userInfo": {}, "userInfoExt": {}},
                    },
                )
                return
            if self.path.endswith("/Friend/GetContractDetail"):
                wxid = body.get("Towxids") or body.get("ToWxIDs") or body.get("Wxid") or "filehelper"
                write_json(
                    self,
                    200,
                    {
                        "Success": True,
                        "Code": 0,
                        "Message": "ok",
                        "Data": {"ContactList": [{"UserName": {"string": wxid}, "NickName": {"string": wxid}}]},
                    },
                )
                return
            write_json(self, 200, {"Success": True, "Code": 0, "Message": "ok", "Data": {}})

        def log_message(self, fmt: str, *args: Any) -> None:
            return

    return WeChatHandler


def serve(server: ThreadingHTTPServer) -> threading.Thread:
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return thread


def wait_for_main(
    main_url: str,
    timeout: float,
    process: subprocess.Popen[bytes] | None = None,
) -> tuple[bool, dict[str, Any]]:
    deadline = time.time() + timeout
    last: dict[str, Any] = {}
    while time.time() < deadline:
        if process is not None and process.poll() is not None:
            return False, {"process_exited": process.returncode, "last": last}
        status, body = get_json(f"{main_url.rstrip('/')}/api/v1/robot/is-running", 2)
        last = {"status": status, "body": body}
        if status < 300:
            try:
                parsed = json.loads(body)
                last["parsed"] = parsed
                if parsed.get("code") == 200:
                    return True, last
            except json.JSONDecodeError:
                pass
        time.sleep(1)
    return False, last


def stop_process_tree(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
    if os.name == "nt":
        subprocess.run(
            ["taskkill", "/F", "/T", "/PID", str(process.pid)],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()


def start_main_process(args: argparse.Namespace, openclaw_url: str, wechat_host: str) -> subprocess.Popen[bytes]:
    env = os.environ.copy()
    env.update(
        {
            "WECHAT_CLIENT_PORT": str(args.main_port),
            "WECHAT_SERVER_HOST": wechat_host,
            "OPENCLAW_ENABLED": "true",
            "OPENCLAW_BASE_URL": openclaw_url,
            "OPENCLAW_TIMEOUT": str(int(args.timeout)),
            "BOT_NAME": args.bot_name,
            "TRIGGER_MODE": args.trigger_mode,
            "TRIGGER_PREFIX": args.trigger_prefix,
        }
    )
    if args.go_env:
        env["GO_ENV"] = args.go_env
    log_path = args.main_log or os.path.join(tempfile.gettempdir(), "assistant-flow-e2e-main.log")
    log_file = open(log_path, "ab", buffering=0)
    print(json.dumps({"main_log": log_path, "main_command": args.start_main_command}, ensure_ascii=False), flush=True)
    return subprocess.Popen(
        args.start_main_command,
        cwd=args.repo_root,
        env=env,
        stdout=log_file,
        stderr=subprocess.STDOUT,
        shell=True,
    )


def run_self_test(args: argparse.Namespace) -> int:
    recorder = Recorder()
    openclaw = ThreadingHTTPServer(("127.0.0.1", 0), make_openclaw_handler(recorder, args.reply))
    wechat = ThreadingHTTPServer(("127.0.0.1", 0), make_wechat_handler(recorder))
    serve(openclaw)
    serve(wechat)
    openclaw_url = f"http://127.0.0.1:{openclaw.server_port}/api/assistant/chat"
    wechat_url = f"http://127.0.0.1:{wechat.server_port}/api/Msg/SendTxt"

    class FakeMainHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            if self.path.endswith("/api/v1/robot/is-running") or self.path.endswith("/api/v1/robot/is-loggedin"):
                write_json(self, 200, {"code": 200, "data": True, "message": ""})
                return
            write_json(self, 404, {"ok": False})

        def do_POST(self) -> None:  # noqa: N802
            _ = read_json_body(self)
            status, body = post_json(openclaw_url, {"message": args.content}, args.timeout)
            reply = json.loads(body).get("reply", "") if status == 200 else ""
            post_json(
                wechat_url,
                {"Wxid": args.wechat_id, "ToWxid": args.from_wxid, "Content": reply, "At": ""},
                args.timeout,
            )
            write_json(self, 200, {"ok": True})

        def log_message(self, fmt: str, *args: Any) -> None:
            return

    fake_main = ThreadingHTTPServer(("127.0.0.1", 0), FakeMainHandler)
    serve(fake_main)
    try:
        return run_smoke(args, recorder, f"http://127.0.0.1:{fake_main.server_port}")
    finally:
        fake_main.shutdown()
        openclaw.shutdown()
        wechat.shutdown()


def run_smoke(args: argparse.Namespace, recorder: Recorder, main_url: str) -> int:
    preflight = {}
    for name, path in {
        "is_running": "/api/v1/robot/is-running",
        "is_loggedin": "/api/v1/robot/is-loggedin",
    }.items():
        status, body = get_json(f"{main_url.rstrip('/')}{path}", args.timeout)
        preflight[name] = {"status": status, "body": body}
    payload = build_sync_callback(
        wechat_id=args.wechat_id,
        from_wxid=args.from_wxid,
        to_wxid=args.to_wxid,
        content=args.content,
        sender_wxid=args.sender_wxid,
        at_wxid=args.at_wxid,
    )
    callback_url = f"{main_url.rstrip('/')}/api/v1/wechat-client/{args.wechat_id}/sync-message"
    status, body = post_json(callback_url, payload, args.timeout)
    deadline = time.time() + args.wait_reply_seconds
    sent: dict[str, Any] | None = None
    while time.time() < deadline:
        try:
            sent = recorder.sent_messages.get(timeout=0.2)
            break
        except queue.Empty:
            continue

    result = {
        "ok": bool(status < 300 and sent),
        "main_callback_status": status,
        "main_callback_body": body,
        "sent_message": sent,
        "openclaw_request_count": len(recorder.openclaw_requests),
        "expected_reply": args.reply,
        "main_preflight": preflight,
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    if not sent:
        print(
            "No /api/Msg/SendTxt call was observed. Check that the main service was started "
            "with WECHAT_SERVER_HOST, OPENCLAW_ENABLED=true, OPENCLAW_BASE_URL, a non-empty active "
            "robot wxid matching --wechat-id, and AI chat enabled. If main_callback_status is 200 "
            "but openclaw_request_count is 0, the callback was accepted by HTTP but did not reach "
            "assistant processing.",
            file=sys.stderr,
        )
        return 1
    content = str(sent.get("Content", ""))
    if args.reply not in content:
        print(f"Observed send content does not contain expected reply {args.reply!r}.", file=sys.stderr)
        return 1
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Smoke-test the assistant flow against local mocks.")
    parser.add_argument("--main-url", default="http://127.0.0.1:9001")
    parser.add_argument("--main-port", type=int, default=9002)
    parser.add_argument("--repo-root", default=os.getcwd())
    parser.add_argument("--start-main-command", default="", help="optional command, e.g. 'go run .'")
    parser.add_argument("--main-start-timeout", type=float, default=90.0)
    parser.add_argument("--main-log", default="")
    parser.add_argument("--go-env", default="dev")
    parser.add_argument("--wechat-port", type=int, default=3022)
    parser.add_argument("--openclaw-port", type=int, default=18791)
    parser.add_argument("--wechat-id", default="wechat_ui_bot")
    parser.add_argument("--from-wxid", default="filehelper")
    parser.add_argument("--to-wxid", default="wechat_ui_bot")
    parser.add_argument("--sender-wxid", default="")
    parser.add_argument("--at-wxid", default="")
    parser.add_argument("--content", default="assistant flow e2e smoke")
    parser.add_argument("--reply", default="OpenClaw mock reply")
    parser.add_argument("--bot-name", default="助手")
    parser.add_argument("--trigger-mode", default="at_or_prefix")
    parser.add_argument("--trigger-prefix", default="助手：")
    parser.add_argument("--timeout", type=float, default=5.0)
    parser.add_argument("--wait-reply-seconds", type=float, default=20.0)
    parser.add_argument("--inject-delay-seconds", type=float, default=0.0)
    parser.add_argument("--hold-mocks", action="store_true", help="start mocks and wait until Ctrl+C")
    parser.add_argument("--self-test", action="store_true", help="verify the harness with a fake main service")
    args = parser.parse_args()

    if args.self_test:
        return run_self_test(args)

    recorder = Recorder()
    openclaw = ThreadingHTTPServer(("127.0.0.1", args.openclaw_port), make_openclaw_handler(recorder, args.reply))
    wechat = ThreadingHTTPServer(("127.0.0.1", args.wechat_port), make_wechat_handler(recorder))
    wechat.wechat_id = args.wechat_id  # type: ignore[attr-defined]
    serve(openclaw)
    serve(wechat)
    openclaw_url = f"http://127.0.0.1:{args.openclaw_port}/api/assistant/chat"
    wechat_host = f"127.0.0.1:{args.wechat_port}"
    main_process: subprocess.Popen[bytes] | None = None
    print(
        json.dumps(
            {
                "mock_openclaw_url": openclaw_url,
                "mock_wechat_server_host": wechat_host,
                "main_url": args.main_url if not args.start_main_command else f"http://127.0.0.1:{args.main_port}",
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    try:
        if args.hold_mocks:
            print("Mocks are running. Press Ctrl+C to stop.", file=sys.stderr)
            while True:
                time.sleep(1)
        if args.start_main_command:
            args.main_url = f"http://127.0.0.1:{args.main_port}"
            main_process = start_main_process(args, openclaw_url, wechat_host)
            ready, state = wait_for_main(args.main_url, args.main_start_timeout, main_process)
            if not ready:
                print(json.dumps({"main_ready": False, "state": state}, ensure_ascii=False, indent=2), file=sys.stderr)
                return 1
            print(json.dumps({"main_ready": True, "state": state}, ensure_ascii=False, indent=2), flush=True)
        if args.inject_delay_seconds > 0:
            print(f"Waiting {args.inject_delay_seconds} seconds before injection.", file=sys.stderr)
            time.sleep(args.inject_delay_seconds)
        return run_smoke(args, recorder, args.main_url)
    finally:
        if main_process is not None:
            stop_process_tree(main_process)
        openclaw.shutdown()
        wechat.shutdown()


if __name__ == "__main__":
    raise SystemExit(main())
