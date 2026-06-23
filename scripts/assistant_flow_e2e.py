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


def configure_stdio() -> None:
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


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


class LocalDBPatch:
    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.robot_row: list[str] | None = None
        self.global_row: list[str] | None = None
        self.chat_room_row: list[str] | None = None
        self.chat_room_member_row: list[str] | None = None
        self.inserted_global_id: str = ""
        self.inserted_chat_room_id: str = ""
        self.inserted_chat_room_member_id: str = ""

    def expected_session_request_text(self) -> str:
        explicit = strings_trim(self.args.expect_session_request_text)
        if explicit:
            return explicit
        content = strings_trim(self.args.content)
        prefix = strings_trim(self.args.trigger_prefix)
        if prefix and content.startswith(prefix):
            return strings_trim(content[len(prefix):])
        return self.args.content

    def mysql(self, sql: str) -> str:
        cmd = [
            "docker",
            "exec",
            self.args.mysql_container,
            "mysql",
            f"-u{self.args.mysql_user}",
            f"-p{self.args.mysql_password}",
            "-N",
            "-B",
            "-e",
            sql,
        ]
        result = subprocess.run(cmd, capture_output=True, text=True, check=False)
        if result.returncode != 0:
            raise RuntimeError(result.stderr.strip() or result.stdout.strip() or "mysql command failed")
        return result.stdout.strip()

    def apply(self) -> None:
        robot_sql = (
            "SELECT COALESCE(wechat_id,''), COALESCE(status,''), COALESCE(CAST(redis_db AS CHAR),'') "
            f"FROM {quote_ident(self.args.admin_db)}.robot WHERE id={int(self.args.robot_id)};"
        )
        robot_out = self.mysql(robot_sql)
        if not robot_out:
            raise RuntimeError(f"robot id {self.args.robot_id} was not found in {self.args.admin_db}.robot")
        self.robot_row = robot_out.split("\t")

        global_sql = (
            "SELECT id, IF(chat_ai_enabled IS NULL, 'NULL', CAST(chat_ai_enabled AS CHAR)) "
            f"FROM {quote_ident(self.args.robot_db)}.global_settings ORDER BY id LIMIT 1;"
        )
        global_out = self.mysql(global_sql)
        self.global_row = global_out.split("\t") if global_out else None

        self.mysql(
            f"UPDATE {quote_ident(self.args.admin_db)}.robot "
            f"SET wechat_id={sql_string(self.args.wechat_id)}, status='online' "
            f"WHERE id={int(self.args.robot_id)};"
        )
        if self.global_row:
            self.mysql(
                f"UPDATE {quote_ident(self.args.robot_db)}.global_settings "
                f"SET chat_ai_enabled=1 WHERE id={int(self.global_row[0])};"
            )
        else:
            self.mysql(f"INSERT INTO {quote_ident(self.args.robot_db)}.global_settings (chat_ai_enabled) VALUES (1);")
            self.inserted_global_id = self.mysql(
                f"SELECT id FROM {quote_ident(self.args.robot_db)}.global_settings ORDER BY id DESC LIMIT 1;"
            ).strip()

        if self.args.from_wxid.endswith("@chatroom"):
            chat_room_sql = (
                "SELECT id, IF(chat_ai_enabled IS NULL, 'NULL', CAST(chat_ai_enabled AS CHAR)) "
                f"FROM {quote_ident(self.args.robot_db)}.chat_room_settings "
                f"WHERE chat_room_id={sql_string(self.args.from_wxid)} ORDER BY id LIMIT 1;"
            )
            chat_room_out = self.mysql(chat_room_sql)
            self.chat_room_row = chat_room_out.split("\t") if chat_room_out else None
            if self.chat_room_row:
                self.mysql(
                    f"UPDATE {quote_ident(self.args.robot_db)}.chat_room_settings "
                    f"SET chat_ai_enabled=1 WHERE id={int(self.chat_room_row[0])};"
                )
            else:
                self.mysql(
                    f"INSERT INTO {quote_ident(self.args.robot_db)}.chat_room_settings "
                    "(chat_room_id, chat_ai_enabled, wxhb_notify_member_list) "
                    f"VALUES ({sql_string(self.args.from_wxid)}, 1, '');"
                )
                self.inserted_chat_room_id = self.mysql(
                    f"SELECT id FROM {quote_ident(self.args.robot_db)}.chat_room_settings "
                    f"WHERE chat_room_id={sql_string(self.args.from_wxid)} ORDER BY id DESC LIMIT 1;"
                ).strip()
            if self.args.sender_wxid:
                member_sql = (
                    "SELECT id, CAST(is_blacklisted AS CHAR), IF(is_leaved IS NULL, 'NULL', CAST(is_leaved AS CHAR)) "
                    f"FROM {quote_ident(self.args.robot_db)}.chat_room_members "
                    f"WHERE chat_room_id={sql_string(self.args.from_wxid)} "
                    f"AND wechat_id={sql_string(self.args.sender_wxid)} ORDER BY id LIMIT 1;"
                )
                member_out = self.mysql(member_sql)
                self.chat_room_member_row = member_out.split("\t") if member_out else None
                if self.chat_room_member_row:
                    self.mysql(
                        f"UPDATE {quote_ident(self.args.robot_db)}.chat_room_members "
                        f"SET is_blacklisted=0, is_leaved=0, last_active_at=UNIX_TIMESTAMP() "
                        f"WHERE id={int(self.chat_room_member_row[0])};"
                    )
                else:
                    self.mysql(
                        f"INSERT INTO {quote_ident(self.args.robot_db)}.chat_room_members "
                        "(chat_room_id, wechat_id, alias, nickname, avatar, inviter_wechat_id, "
                        "is_admin, is_blacklisted, is_leaved, score, temporary_score, temporary_score_expiry, "
                        "remark, joined_at, last_active_at) "
                        f"VALUES ({sql_string(self.args.from_wxid)}, {sql_string(self.args.sender_wxid)}, "
                        "'', 'E2E Group User', '', '', 0, 0, 0, 0, 0, 0, '', UNIX_TIMESTAMP(), UNIX_TIMESTAMP());"
                    )
                    self.inserted_chat_room_member_id = self.mysql(
                        f"SELECT id FROM {quote_ident(self.args.robot_db)}.chat_room_members "
                        f"WHERE chat_room_id={sql_string(self.args.from_wxid)} "
                        f"AND wechat_id={sql_string(self.args.sender_wxid)} ORDER BY id DESC LIMIT 1;"
                    ).strip()

        print(
            json.dumps(
                {
                    "local_db_patch": "applied",
                    "robot_id": self.args.robot_id,
                    "wechat_id": self.args.wechat_id,
                    "global_chat_ai_enabled": True,
                    "chat_room_ai_enabled": self.args.from_wxid.endswith("@chatroom"),
                },
                ensure_ascii=False,
            ),
            flush=True,
        )

    def restore(self) -> None:
        errors: list[str] = []
        expected_request_text = self.expected_session_request_text()
        request_text_values = sorted({self.args.content, expected_request_text})
        cleanup_sqls = [
            f"DELETE FROM {quote_ident(self.args.robot_db)}.assistant_session_logs "
            f"WHERE request_text IN ({','.join(sql_string(value) for value in request_text_values)});",
            f"DELETE FROM {quote_ident(self.args.robot_db)}.messages "
            f"WHERE from_wxid={sql_string(self.args.from_wxid)} "
            f"AND to_wxid={sql_string(self.args.wechat_id)} "
            f"AND content={sql_string(self.args.content)};",
            f"DELETE FROM {quote_ident(self.args.robot_db)}.messages "
            f"WHERE from_wxid={sql_string(self.args.from_wxid)} "
            f"AND to_wxid={sql_string(self.args.wechat_id)} "
            f"AND content={sql_string(self.args.reply)};",
            f"DELETE FROM {quote_ident(self.args.robot_db)}.contacts "
            f"WHERE wechat_id={sql_string(self.args.from_wxid)};",
        ]
        if self.args.sender_wxid:
            cleanup_sqls.append(
                f"DELETE FROM {quote_ident(self.args.robot_db)}.contacts "
                f"WHERE wechat_id={sql_string(self.args.sender_wxid)};"
            )
        for sql in cleanup_sqls:
            try:
                self.mysql(sql)
            except Exception as exc:  # pragma: no cover - cleanup diagnostics only.
                errors.append(str(exc))
        if self.robot_row is not None:
            old_wechat_id = self.robot_row[0] if len(self.robot_row) > 0 else ""
            old_status = self.robot_row[1] if len(self.robot_row) > 1 else ""
            try:
                self.mysql(
                    f"UPDATE {quote_ident(self.args.admin_db)}.robot "
                    f"SET wechat_id={sql_string(old_wechat_id)}, status={sql_string(old_status)} "
                    f"WHERE id={int(self.args.robot_id)};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        if self.inserted_global_id:
            try:
                self.mysql(
                    f"DELETE FROM {quote_ident(self.args.robot_db)}.global_settings "
                    f"WHERE id={int(self.inserted_global_id)};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        elif self.global_row is not None:
            old_value = self.global_row[1] if len(self.global_row) > 1 else "NULL"
            restore_value = "NULL" if old_value == "NULL" else str(int(old_value))
            try:
                self.mysql(
                    f"UPDATE {quote_ident(self.args.robot_db)}.global_settings "
                    f"SET chat_ai_enabled={restore_value} WHERE id={int(self.global_row[0])};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        if self.inserted_chat_room_id:
            try:
                self.mysql(
                    f"DELETE FROM {quote_ident(self.args.robot_db)}.chat_room_settings "
                    f"WHERE id={int(self.inserted_chat_room_id)};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        elif self.chat_room_row is not None:
            old_value = self.chat_room_row[1] if len(self.chat_room_row) > 1 else "NULL"
            restore_value = "NULL" if old_value == "NULL" else str(int(old_value))
            try:
                self.mysql(
                    f"UPDATE {quote_ident(self.args.robot_db)}.chat_room_settings "
                    f"SET chat_ai_enabled={restore_value} WHERE id={int(self.chat_room_row[0])};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        if self.inserted_chat_room_member_id:
            try:
                self.mysql(
                    f"DELETE FROM {quote_ident(self.args.robot_db)}.chat_room_members "
                    f"WHERE id={int(self.inserted_chat_room_member_id)};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        elif self.chat_room_member_row is not None:
            old_blacklisted = self.chat_room_member_row[1] if len(self.chat_room_member_row) > 1 else "0"
            old_leaved = self.chat_room_member_row[2] if len(self.chat_room_member_row) > 2 else "NULL"
            restore_leaved = "NULL" if old_leaved == "NULL" else str(int(old_leaved))
            try:
                self.mysql(
                    f"UPDATE {quote_ident(self.args.robot_db)}.chat_room_members "
                    f"SET is_blacklisted={int(old_blacklisted)}, is_leaved={restore_leaved} "
                    f"WHERE id={int(self.chat_room_member_row[0])};"
                )
            except Exception as exc:  # pragma: no cover - restoration diagnostics only.
                errors.append(str(exc))
        print(
            json.dumps(
                {"local_db_patch": "restored", "errors": errors},
                ensure_ascii=False,
            ),
            flush=True,
        )

    def verify_session_log(self) -> dict[str, Any]:
        raw = self.mysql(
            "SELECT JSON_OBJECT("
            "'status', status, "
            "'reply_text', COALESCE(reply_text,''), "
            "'error_message', COALESCE(error_message,''), "
            "'request_text', COALESCE(request_text,''), "
            "'openclaw_url', COALESCE(openclaw_url,'')) "
            f"FROM {quote_ident(self.args.robot_db)}.assistant_session_logs "
            f"WHERE request_text={sql_string(self.expected_session_request_text())} "
            "ORDER BY id DESC LIMIT 1;"
        )
        if not raw:
            raise RuntimeError("assistant_session_logs did not contain a row for the injected request_text")
        try:
            row = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"assistant_session_logs verification returned invalid JSON: {raw}") from exc
        if row.get("status") != "success":
            raise RuntimeError(f"assistant_session_logs status was not success: {row}")
        if self.args.reply not in str(row.get("reply_text", "")):
            raise RuntimeError(f"assistant_session_logs reply_text did not contain expected reply: {row}")
        print(json.dumps({"assistant_session_log": "verified", "row": row}, ensure_ascii=False), flush=True)
        return row


def quote_ident(value: str) -> str:
    if not value.replace("_", "").isalnum():
        raise ValueError(f"unsafe SQL identifier: {value!r}")
    return "`" + value.replace("`", "``") + "`"


def sql_string(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def strings_trim(value: str) -> str:
    return str(value or "").strip()


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


def make_wechat_handler(recorder: Recorder, send_error_code: str = "") -> type[BaseHTTPRequestHandler]:
    class WeChatHandler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:  # noqa: N802
            body = read_json_body(self)
            if self.path.endswith("/Msg/SendTxt"):
                recorder.sent_messages.put(body)
                print(json.dumps({"mock_wechat_send": body}, ensure_ascii=False), flush=True)
                now = int(time.time())
                if send_error_code == "blocking_window":
                    write_json(
                        self,
                        409,
                        {
                            "Success": False,
                            "Code": -5,
                            "Message": "WeChat UI blocked: blocking_window",
                            "Data": {
                                "error_code": "blocking_window",
                                "blocking_windows": [
                                    {
                                        "name": "Windows Security Alert",
                                        "class_name": "#32770",
                                        "rect": "566,230,1352,849",
                                    }
                                ],
                            },
                        },
                    )
                    return
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


def wait_for_file_text(path: str, needle: str, timeout: float) -> tuple[bool, dict[str, Any]]:
    deadline = time.time() + timeout
    last: dict[str, Any] = {}
    while time.time() < deadline:
        try:
            with open(path, "r", encoding="utf-8", errors="replace") as fh:
                content = fh.read()
            last = {"path": path, "size": len(content), "tail": content[-2000:]}
            if needle in content:
                return True, last
        except FileNotFoundError:
            last = {"path": path, "error": "file not found"}
        except OSError as exc:
            last = {"path": path, "error": str(exc)}
        time.sleep(0.5)
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
    args.main_log = log_path
    log_file = open(log_path, "wb", buffering=0)
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
        "mock_wechat_send_error": args.mock_wechat_send_error,
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
    configure_stdio()
    parser = argparse.ArgumentParser(description="Smoke-test the assistant flow against local mocks.")
    parser.add_argument("--main-url", default="http://127.0.0.1:9001")
    parser.add_argument("--main-port", type=int, default=9002)
    parser.add_argument("--repo-root", default=os.getcwd())
    parser.add_argument("--start-main-command", default="", help="optional command, e.g. 'go run .'")
    parser.add_argument("--main-start-timeout", type=float, default=90.0)
    parser.add_argument("--main-log", default="")
    parser.add_argument(
        "--expect-main-log-text",
        default="",
        help="after a successful send attempt, require the main-service log to contain this text",
    )
    parser.add_argument("--expect-main-log-timeout", type=float, default=10.0)
    parser.add_argument("--go-env", default="dev")
    parser.add_argument("--prepare-local-db", action="store_true")
    parser.add_argument(
        "--verify-session-log",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="when --prepare-local-db is active, verify assistant_session_logs before cleanup",
    )
    parser.add_argument("--mysql-container", default="wechat-admin-mysql")
    parser.add_argument("--mysql-user", default="root")
    parser.add_argument("--mysql-password", default="mroot12345678")
    parser.add_argument("--admin-db", default="robot_admin")
    parser.add_argument("--robot-db", default="openclaw_assistant_dev")
    parser.add_argument("--robot-id", type=int, default=27)
    parser.add_argument("--wechat-port", type=int, default=3022)
    parser.add_argument("--mock-wechat-send-error", choices=["", "blocking_window"], default="")
    parser.add_argument("--openclaw-port", type=int, default=18791)
    parser.add_argument("--wechat-id", default="wechat_ui_bot")
    parser.add_argument("--from-wxid", default="wxid_e2e_friend")
    parser.add_argument("--to-wxid", default="wechat_ui_bot")
    parser.add_argument("--sender-wxid", default="")
    parser.add_argument("--at-wxid", default="")
    parser.add_argument("--content", default="assistant flow e2e smoke")
    parser.add_argument("--reply", default="OpenClaw mock reply")
    parser.add_argument("--bot-name", default="助手")
    parser.add_argument("--trigger-mode", default="at_or_prefix")
    parser.add_argument("--trigger-prefix", default="助手：")
    parser.add_argument(
        "--expect-session-request-text",
        default="",
        help="optional assistant_session_logs.request_text override; defaults to content with trigger prefix removed",
    )
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
    wechat = ThreadingHTTPServer(
        ("127.0.0.1", args.wechat_port),
        make_wechat_handler(recorder, args.mock_wechat_send_error),
    )
    wechat.wechat_id = args.wechat_id  # type: ignore[attr-defined]
    serve(openclaw)
    serve(wechat)
    openclaw_url = f"http://127.0.0.1:{args.openclaw_port}/api/assistant/chat"
    wechat_host = f"127.0.0.1:{args.wechat_port}"
    main_process: subprocess.Popen[bytes] | None = None
    db_patch: LocalDBPatch | None = None
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
        if args.prepare_local_db:
            db_patch = LocalDBPatch(args)
            db_patch.apply()
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
        exit_code = run_smoke(args, recorder, args.main_url)
        if exit_code == 0 and args.expect_main_log_text:
            if not args.main_log:
                print("--expect-main-log-text requires --main-log or --start-main-command.", file=sys.stderr)
                return 1
            found, log_state = wait_for_file_text(
                args.main_log,
                args.expect_main_log_text,
                args.expect_main_log_timeout,
            )
            print(
                json.dumps(
                    {
                        "main_log_text": "verified" if found else "missing",
                        "needle": args.expect_main_log_text,
                        "state": log_state,
                    },
                    ensure_ascii=False,
                    indent=2,
                ),
                flush=True,
            )
            if not found:
                return 1
        if exit_code == 0 and args.verify_session_log and db_patch is not None:
            db_patch.verify_session_log()
        return exit_code
    finally:
        if main_process is not None:
            stop_process_tree(main_process)
        if db_patch is not None:
            db_patch.restore()
        openclaw.shutdown()
        wechat.shutdown()


if __name__ == "__main__":
    raise SystemExit(main())
