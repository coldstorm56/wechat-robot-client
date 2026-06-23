#!/usr/bin/env python
"""Local smoke tool for WeChat 4.x UI automation.

This script intentionally stays local-only. It uses Windows UI Automation and
the logged-in desktop WeChat client; it does not read credentials or talk to a
public endpoint.
"""

from __future__ import annotations

import argparse
import contextlib
import json
import logging
import os
import subprocess
import sys
import tempfile
import time
from dataclasses import asdict, dataclass
from typing import Iterable

try:
    import uiautomation as auto
    import win32clipboard
    import win32con
    import win32gui
    import win32process
except ImportError as exc:  # pragma: no cover - host dependency preflight
    print(
        json.dumps(
            {
                "ok": False,
                "error": f"missing Python UI automation dependency: {exc}",
            },
            ensure_ascii=False,
        )
    )
    raise SystemExit(2)


DEFAULT_WECHAT_EXE = r"D:\software\Weixin\Weixin.exe"
DEFAULT_CONTACT = "\u6587\u4ef6\u4f20\u8f93\u52a9\u624b"


@dataclass
class WindowInfo:
    hwnd: int
    name: str
    class_name: str
    process_id: int
    rect: str


def configure_logging() -> None:
    log_path = os.environ.get(
        "WECHAT_UI_LOG_PATH",
        os.path.join(tempfile.gettempdir(), "wechat-ui-automation-bridge.log"),
    )
    logging.basicConfig(
        filename=log_path,
        level=logging.INFO,
        format="%(asctime)s %(levelname)s %(message)s",
    )


def json_print(payload: dict) -> None:
    print(json.dumps(payload, ensure_ascii=False, indent=2))


def get_clipboard_text() -> str:
    try:
        win32clipboard.OpenClipboard()
        if not win32clipboard.IsClipboardFormatAvailable(win32con.CF_UNICODETEXT):
            return ""
        return win32clipboard.GetClipboardData(win32con.CF_UNICODETEXT)
    except Exception as exc:  # pragma: no cover - depends on desktop state
        logging.warning("read clipboard failed: %s", exc)
        return ""
    finally:
        with contextlib.suppress(Exception):
            win32clipboard.CloseClipboard()


def set_clipboard_text(text: str) -> None:
    win32clipboard.OpenClipboard()
    try:
        win32clipboard.EmptyClipboard()
        win32clipboard.SetClipboardData(win32con.CF_UNICODETEXT, text)
    finally:
        win32clipboard.CloseClipboard()


@contextlib.contextmanager
def preserved_desktop_state(restore_window: bool = True):
    old_clipboard = get_clipboard_text()
    old_foreground = win32gui.GetForegroundWindow()
    try:
        yield
    finally:
        with contextlib.suppress(Exception):
            set_clipboard_text(old_clipboard)
        if restore_window and old_foreground:
            with contextlib.suppress(Exception):
                win32gui.SetForegroundWindow(old_foreground)


def launch_wechat(exe_path: str) -> None:
    if not os.path.exists(exe_path):
        raise FileNotFoundError(f"WeChat executable not found: {exe_path}")
    subprocess.Popen([exe_path], close_fds=True)


def is_wechat_window_name(name: str, class_name: str) -> bool:
    if class_name in {"CabinetWClass", "ExploreWClass"}:
        return False
    return (
        "微信" in name
        or "WeChat" in name
        or "mmui" in class_name
        or "WeChat" in class_name
        or "Weixin" in class_name
    )


def iter_wechat_window_handles() -> Iterable[int]:
    handles: list[int] = []

    def callback(hwnd: int, _extra: object) -> bool:
        if not win32gui.IsWindow(hwnd) or not win32gui.IsWindowVisible(hwnd):
            return True
        name = (win32gui.GetWindowText(hwnd) or "").strip()
        class_name = (win32gui.GetClassName(hwnd) or "").strip()
        if is_wechat_window_name(name, class_name):
            handles.append(hwnd)
        return True

    win32gui.EnumWindows(callback, None)
    return handles


def get_wechat_windows() -> list[auto.Control]:
    controls: list[auto.Control] = []
    for hwnd in iter_wechat_window_handles():
        with contextlib.suppress(Exception):
            controls.append(auto.ControlFromHandle(hwnd))
    return controls


def describe_window(ctrl: auto.Control) -> WindowInfo:
    rect = ctrl.BoundingRectangle
    hwnd = int(ctrl.NativeWindowHandle or 0)
    process_id = int(ctrl.ProcessId or 0)
    if hwnd and not process_id:
        with contextlib.suppress(Exception):
            _, process_id = win32process.GetWindowThreadProcessId(hwnd)
    return WindowInfo(
        hwnd=hwnd,
        name=ctrl.Name or "",
        class_name=ctrl.ClassName or "",
        process_id=process_id,
        rect=f"{rect.left},{rect.top},{rect.right},{rect.bottom}",
    )


def find_wechat_window(timeout: float, launch: bool, exe_path: str) -> auto.Control:
    deadline = time.time() + timeout
    launched = False
    while time.time() < deadline:
        windows = get_wechat_windows()
        if windows:
            return windows[0]
        if launch and not launched:
            launch_wechat(exe_path)
            launched = True
        time.sleep(0.5)
    raise TimeoutError("WeChat 4.x window was not found")


def activate(ctrl: auto.Control) -> None:
    ctrl.SetActive()
    time.sleep(0.5)


def paste_text(text: str) -> None:
    set_clipboard_text(text)
    auto.SendKeys("{Ctrl}v", waitTime=0.05)
    time.sleep(0.2)


def open_contact(ctrl: auto.Control, contact: str) -> None:
    activate(ctrl)
    auto.SendKeys("{Ctrl}f", waitTime=0.05)
    time.sleep(0.2)
    paste_text(contact)
    time.sleep(0.6)
    auto.SendKeys("{Enter}", waitTime=0.05)
    time.sleep(0.8)


def send_text(ctrl: auto.Control, contact: str, message: str) -> None:
    open_contact(ctrl, contact)
    paste_text(message)
    time.sleep(0.2)
    auto.SendKeys("{Enter}", waitTime=0.05)
    logging.info("sent text via UI automation contact=%r length=%d", contact, len(message))


def collect_texts(ctrl: auto.Control, limit: int = 80) -> list[str]:
    texts: list[str] = []
    for item in ctrl.GetChildren():
        texts.extend(collect_texts_deep(item, limit - len(texts)))
        if len(texts) >= limit:
            break
    return texts[-limit:]


def collect_texts_deep(ctrl: auto.Control, remaining: int) -> list[str]:
    if remaining <= 0:
        return []
    values: list[str] = []
    name = (ctrl.Name or "").strip()
    if name and len(name) <= 200:
        values.append(name)
    for child in ctrl.GetChildren():
        if len(values) >= remaining:
            break
        values.extend(collect_texts_deep(child, remaining - len(values)))
    return values


def read_last_text(ctrl: auto.Control, contact: str | None) -> str:
    if contact:
        open_contact(ctrl, contact)
    activate(ctrl)
    texts = [text for text in collect_texts(ctrl) if text.strip()]
    ignored = {
        "微信",
        "通讯录",
        "收藏",
        "聊天",
        "文件传输助手",
        "搜索",
    }
    candidates = [text for text in texts if text not in ignored]
    if not candidates:
        raise RuntimeError("no readable text found in current WeChat conversation")
    return candidates[-1]


def cmd_inspect(args: argparse.Namespace) -> int:
    if args.launch:
        with contextlib.suppress(Exception):
            find_wechat_window(1, True, args.wechat_exe)
    windows = [describe_window(ctrl) for ctrl in get_wechat_windows()]
    json_print(
        {
            "ok": bool(windows),
            "windows": [asdict(window) for window in windows],
            "dependency": "uiautomation",
        }
    )
    return 0 if windows else 1


def cmd_send(args: argparse.Namespace) -> int:
    with preserved_desktop_state(restore_window=not args.keep_focus):
        window = find_wechat_window(args.timeout, args.launch, args.wechat_exe)
        send_text(window, args.contact, args.message)
    json_print({"ok": True, "contact": args.contact, "sent": args.message})
    return 0


def cmd_read_last(args: argparse.Namespace) -> int:
    with preserved_desktop_state(restore_window=not args.keep_focus):
        window = find_wechat_window(args.timeout, args.launch, args.wechat_exe)
        text = read_last_text(window, args.contact)
    json_print({"ok": True, "contact": args.contact, "last_text": text})
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="WeChat 4.x local UI automation smoke tool")
    parser.add_argument("--wechat-exe", default=DEFAULT_WECHAT_EXE)
    parser.add_argument("--timeout", type=float, default=8)
    parser.add_argument("--launch", action="store_true", help="start WeChat when no window is found")
    parser.add_argument("--keep-focus", action="store_true", help="leave WeChat focused after the action")
    sub = parser.add_subparsers(dest="command", required=True)

    inspect = sub.add_parser("inspect", help="list detected WeChat UIA windows")
    inspect.set_defaults(func=cmd_inspect)

    send = sub.add_parser("send", help="send a text message through the WeChat UI")
    send.add_argument("--contact", default=DEFAULT_CONTACT)
    send.add_argument("--message", required=True)
    send.set_defaults(func=cmd_send)

    read_last = sub.add_parser("read-last", help="read the last visible text from a conversation")
    read_last.add_argument("--contact", default=None)
    read_last.set_defaults(func=cmd_read_last)

    return parser


def main() -> int:
    configure_logging()
    parser = build_parser()
    args = parser.parse_args()
    try:
        return args.func(args)
    except Exception as exc:
        logging.exception("wechat ui automation failed")
        json_print({"ok": False, "error": str(exc)})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
