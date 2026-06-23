#!/usr/bin/env python
"""Local smoke tool for WeChat 4.x UI automation.

This script intentionally stays local-only. It uses Windows UI Automation and
the logged-in desktop WeChat client; it does not read credentials or talk to a
public endpoint.
"""

from __future__ import annotations

import argparse
import contextlib
import io
import json
import logging
import os
import re
import subprocess
import sys
import tempfile
import time
from dataclasses import asdict, dataclass
from typing import Iterable

try:
    import uiautomation as auto
    import win32api
    import win32clipboard
    import win32con
    import win32gui
    import win32process
    from PIL import ImageGrab
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


@dataclass
class UIStatus:
    window: WindowInfo
    title_text: str
    title_match: bool | None
    chat_text_sample: list[str]
    foreground: bool


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
    controls.sort(key=window_priority)
    return controls


def window_priority(ctrl: auto.Control) -> tuple[int, int]:
    class_name = ctrl.ClassName or ""
    rect = ctrl.BoundingRectangle
    area = max(0, rect.right - rect.left) * max(0, rect.bottom - rect.top)
    if class_name == "Chrome_WidgetWin_0" and area > 0:
        return (0, -area)
    if area > 0:
        return (1, -area)
    return (2, 0)


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
    restore_control_window(ctrl)
    ensure_usable_chat_window(ctrl)
    ctrl.SetActive()
    time.sleep(0.5)


def restore_control_window(ctrl: auto.Control) -> None:
    hwnd = int(ctrl.NativeWindowHandle or 0)
    if not hwnd:
        return
    if win32gui.IsIconic(hwnd):
        win32gui.ShowWindow(hwnd, win32con.SW_RESTORE)
        time.sleep(0.3)
    force_foreground_window(hwnd)


def force_foreground_window(hwnd: int) -> None:
    win32gui.ShowWindow(hwnd, win32con.SW_SHOW)
    win32gui.SetWindowPos(
        hwnd,
        win32con.HWND_TOP,
        0,
        0,
        0,
        0,
        win32con.SWP_NOMOVE | win32con.SWP_NOSIZE | win32con.SWP_SHOWWINDOW,
    )
    current_thread = win32api.GetCurrentThreadId()
    target_thread, _ = win32process.GetWindowThreadProcessId(hwnd)
    foreground = win32gui.GetForegroundWindow()
    foreground_thread = 0
    if foreground:
        foreground_thread, _ = win32process.GetWindowThreadProcessId(foreground)

    attached_threads: list[int] = []
    for thread_id in {target_thread, foreground_thread}:
        if thread_id and thread_id != current_thread:
            with contextlib.suppress(Exception):
                win32process.AttachThreadInput(current_thread, thread_id, True)
                attached_threads.append(thread_id)

    try:
        with contextlib.suppress(Exception):
            win32gui.BringWindowToTop(hwnd)
        with contextlib.suppress(Exception):
            win32gui.SetForegroundWindow(hwnd)
    finally:
        for thread_id in attached_threads:
            with contextlib.suppress(Exception):
                win32process.AttachThreadInput(current_thread, thread_id, False)

    if win32gui.GetForegroundWindow() != hwnd:
        with contextlib.suppress(Exception):
            win32api.keybd_event(win32con.VK_MENU, 0, 0, 0)
            win32api.keybd_event(win32con.VK_MENU, 0, win32con.KEYEVENTF_KEYUP, 0)
            win32gui.SetForegroundWindow(hwnd)
    time.sleep(0.3)


def ensure_usable_chat_window(ctrl: auto.Control) -> None:
    left, top, right, bottom = window_rect(ctrl)
    width = right - left
    height = bottom - top
    class_name = (ctrl.ClassName or "").lower()
    if width < 200 or height < 200 or "login" in class_name:
        raise RuntimeError(
            "WeChat was found, but it is not a usable chat window. "
            "Log in to WeChat 4.x and open the main chat window first."
        )


def window_rect(ctrl: auto.Control) -> tuple[int, int, int, int]:
    hwnd = int(ctrl.NativeWindowHandle or 0)
    if hwnd:
        with contextlib.suppress(Exception):
            return win32gui.GetWindowRect(hwnd)
    rect = ctrl.BoundingRectangle
    return rect.left, rect.top, rect.right, rect.bottom


def screenshot_rect(ctrl: auto.Control) -> tuple[int, int, int, int]:
    rect = ctrl.BoundingRectangle
    left, top, right, bottom = rect.left, rect.top, rect.right, rect.bottom
    if right - left >= 200 and bottom - top >= 200:
        return left, top, right, bottom
    return window_rect(ctrl)


def click_message_input(ctrl: auto.Control) -> None:
    left, top, right, bottom = screenshot_rect(ctrl)
    width = right - left
    height = bottom - top
    x = left + int(width * 0.34)
    y = bottom - int(height * 0.23)
    win32api.SetCursorPos((x, y))
    win32api.mouse_event(win32con.MOUSEEVENTF_LEFTDOWN, 0, 0, 0, 0)
    win32api.mouse_event(win32con.MOUSEEVENTF_LEFTUP, 0, 0, 0, 0)
    time.sleep(0.2)


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


def send_text(
    ctrl: auto.Control,
    contact: str,
    message: str,
    expected_title: str,
    verify_editor: bool,
    editor_verify_timeout: float,
) -> str:
    if contact:
        open_contact(ctrl, contact)
    else:
        activate(ctrl)
    if expected_title:
        verify_conversation_title(ctrl, expected_title)
    click_message_input(ctrl)
    paste_text(message)
    time.sleep(0.2)
    editor_text = ""
    if verify_editor:
        editor_text = verify_editor_draft(ctrl, message, editor_verify_timeout)
    auto.SendKeys("{Enter}", waitTime=0.05)
    logging.info("submitted text via UI automation contact=%r length=%d", contact or "<current>", len(message))
    return editor_text


def verify_editor_draft(ctrl: auto.Control, expected: str, timeout: float) -> str:
    deadline = time.time() + timeout
    last_error = ""
    normalized_expected = normalize_for_match(expected)
    while time.time() < deadline:
        try:
            text = "\n".join(ocr_editor_texts(ctrl))
            if normalized_expected and normalized_expected in normalize_for_match(text):
                return text
            last_error = f"OCR editor text did not contain draft: {text[-300:]!r}"
        except Exception as exc:
            last_error = str(exc)
        time.sleep(0.3)
    with contextlib.suppress(Exception):
        auto.SendKeys("{Ctrl}a", waitTime=0.05)
        auto.SendKeys("{Back}", waitTime=0.05)
    raise RuntimeError(f"message draft did not reach the WeChat editor; Enter was not pressed: {last_error}")


def verify_visible_message(ctrl: auto.Control, expected: str, timeout: float) -> str:
    deadline = time.time() + timeout
    last_error = ""
    normalized_expected = normalize_for_match(expected)
    while time.time() < deadline:
        try:
            text = "\n".join(ocr_chat_texts(ctrl))
            if normalized_expected and normalized_expected in normalize_for_match(text):
                return text
            last_error = f"OCR text did not contain submitted message: {text[-300:]!r}"
        except Exception as exc:
            last_error = str(exc)
        time.sleep(0.5)
    raise RuntimeError(f"message submission attempted but visible delivery was not verified: {last_error}")


def normalize_for_match(text: str) -> str:
    return re.sub(r"[\W_]+", "", text, flags=re.UNICODE).lower()


def contains_normalized(text: str, expected: str) -> bool:
    normalized_expected = normalize_for_match(expected)
    return bool(normalized_expected and normalized_expected in normalize_for_match(text))


def verify_conversation_title(ctrl: auto.Control, expected: str) -> str:
    text = "\n".join(ocr_title_texts(ctrl))
    if contains_normalized(text, expected):
        return text
    raise RuntimeError(f"current WeChat conversation title did not match {expected!r}: {text!r}")


def read_ui_status(ctrl: auto.Control, expected_title: str | None) -> UIStatus:
    activate(ctrl)
    title_texts = ocr_title_texts(ctrl)
    title_text = "\n".join(title_texts)
    title_match = None
    if expected_title:
        title_match = contains_normalized(title_text, expected_title)
    chat_texts: list[str] = []
    with contextlib.suppress(Exception):
        chat_texts = ocr_chat_texts(ctrl)[-12:]
    hwnd = int(ctrl.NativeWindowHandle or 0)
    return UIStatus(
        window=describe_window(ctrl),
        title_text=title_text,
        title_match=title_match,
        chat_text_sample=chat_texts,
        foreground=bool(hwnd and win32gui.GetForegroundWindow() == hwnd),
    )


def ocr_rect_texts(rect: tuple[int, int, int, int]) -> list[str]:
    try:
        from rapidocr import RapidOCR
    except ImportError as exc:
        raise RuntimeError("rapidocr is required for visible send verification") from exc

    left, top, right, bottom = rect
    if right - left < 50 or bottom - top < 20:
        raise RuntimeError("cannot OCR an unusable WeChat window")
    image = ImageGrab.grab((left, top, right, bottom))
    sink = io.StringIO()
    logging.getLogger("RapidOCR").disabled = True
    with contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
        result = RapidOCR()(image)
    texts = []
    for text in getattr(result, "txts", None) or []:
        if text:
            texts.append(str(text))
    return texts


def ocr_window_texts(ctrl: auto.Control) -> list[str]:
    return ocr_rect_texts(screenshot_rect(ctrl))


def ocr_title_texts(ctrl: auto.Control) -> list[str]:
    hwnd = int(ctrl.NativeWindowHandle or 0)
    if hwnd and win32gui.GetForegroundWindow() != hwnd:
        raise RuntimeError("WeChat window is not foreground; refusing to OCR a possibly covered title area")
    left, top, right, bottom = screenshot_rect(ctrl)
    width = right - left
    height = bottom - top
    return ocr_rect_texts(
        (
            left + int(width * 0.30),
            top + int(height * 0.02),
            left + int(width * 0.72),
            top + int(height * 0.10),
        )
    )


def ocr_chat_texts(ctrl: auto.Control) -> list[str]:
    hwnd = int(ctrl.NativeWindowHandle or 0)
    if hwnd and win32gui.GetForegroundWindow() != hwnd:
        raise RuntimeError("WeChat window is not foreground; refusing to OCR a possibly covered chat area")
    left, top, right, bottom = screenshot_rect(ctrl)
    width = right - left
    height = bottom - top
    return ocr_rect_texts(
        (
            left + int(width * 0.30),
            top + int(height * 0.11),
            right - int(width * 0.02),
            bottom - int(height * 0.28),
        )
    )


def ocr_editor_texts(ctrl: auto.Control) -> list[str]:
    hwnd = int(ctrl.NativeWindowHandle or 0)
    if hwnd and win32gui.GetForegroundWindow() != hwnd:
        raise RuntimeError("WeChat window is not foreground; refusing to OCR a possibly covered editor area")
    left, top, right, bottom = screenshot_rect(ctrl)
    width = right - left
    height = bottom - top
    return ocr_rect_texts(
        (
            left + int(width * 0.30),
            top + int(height * 0.72),
            right - int(width * 0.02),
            bottom - int(height * 0.02),
        )
    )


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


def read_last_text(ctrl: auto.Control, contact: str | None, expected_title: str) -> str:
    if contact:
        open_contact(ctrl, contact)
    activate(ctrl)
    if expected_title:
        verify_conversation_title(ctrl, expected_title)
    texts = [text for text in ocr_chat_texts(ctrl) if text.strip()]
    if any("搜一搜" in text for text in texts[:10]):
        raise RuntimeError("current WeChat UIA document is a search page, not a chat message list")
    ignored = {
        "微信",
        "通讯录",
        "收藏",
        "聊天",
        "文件传输助手",
        "搜索",
        "系统",
        "最小化",
        "最大化",
        "关闭",
        "MMUIRenderSubWindow",
    }
    candidates = [
        text
        for text in texts
        if text not in ignored
        and not text.endswith(" - 贴图")
        and not text.endswith(" - 图片")
        and not text.endswith(" - 文件")
        and not is_time_label(text)
        and not re.fullmatch(r"\d{1,2}", text)
    ]
    if not candidates:
        raise RuntimeError("no readable text found in current WeChat conversation")
    return candidates[-1]


def is_time_label(text: str) -> bool:
    return bool(
        re.fullmatch(r"\d{1,2}:\d{2}", text)
        or re.fullmatch(r"\d+\s*(秒|分钟|小时|天|个月|年)前", text)
        or text in {"刚刚", "昨天", "前天", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六", "星期日"}
        or re.fullmatch(r"\d{1,2}月\d{1,2}日\s+\d{1,2}:\d{2}", text)
    )


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


def cmd_status(args: argparse.Namespace) -> int:
    with preserved_desktop_state(restore_window=not args.keep_focus):
        window = find_wechat_window(args.timeout, args.launch, args.wechat_exe)
        status = read_ui_status(window, args.expect_title)
    json_print(
        {
            "ok": True,
            "status": asdict(status),
            "expected_title": args.expect_title,
        }
    )
    return 0


def cmd_send(args: argparse.Namespace) -> int:
    with preserved_desktop_state(restore_window=not args.keep_focus):
        window = find_wechat_window(args.timeout, args.launch, args.wechat_exe)
        draft_text = send_text(
            window,
            args.contact,
            args.message,
            args.expect_title,
            args.verify_editor,
            args.editor_verify_timeout,
        )
        verified_text = ""
        if args.verify:
            verified_text = verify_visible_message(window, args.message, args.verify_timeout)
    json_print(
        {
            "ok": True,
            "contact": args.contact,
            "submitted": args.message,
            "draft_status": "editor_verified" if args.verify_editor else "editor_unverified",
            "draft_text": draft_text,
            "delivery_status": "visible_verified" if args.verify else "submitted_unverified",
            "verified_text": verified_text,
        }
    )
    return 0


def cmd_read_last(args: argparse.Namespace) -> int:
    with preserved_desktop_state(restore_window=not args.keep_focus):
        window = find_wechat_window(args.timeout, args.launch, args.wechat_exe)
        text = read_last_text(window, args.contact, args.expect_title)
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

    status = sub.add_parser("status", help="read current WeChat UI state without touching the editor")
    status.add_argument("--expect-title", default=None)
    status.set_defaults(func=cmd_status)

    send = sub.add_parser("send", help="send a text message through the WeChat UI")
    send.add_argument("--contact", default=None, help="experimental: navigate by contact name before sending")
    send.add_argument("--expect-title", default=None, help="fail before touching the editor unless the current title matches this text")
    send.add_argument("--message", required=True)
    send.add_argument("--verify", action=argparse.BooleanOptionalAction, default=True, help="require the submitted text to become visible before returning ok")
    send.add_argument("--verify-timeout", type=float, default=5)
    send.add_argument("--verify-editor", action=argparse.BooleanOptionalAction, default=True, help="require the draft to be visible in the message editor before pressing Enter")
    send.add_argument("--editor-verify-timeout", type=float, default=3)
    send.set_defaults(func=cmd_send)

    read_last = sub.add_parser("read-last", help="read the last visible text from a conversation")
    read_last.add_argument("--contact", default=None)
    read_last.add_argument("--expect-title", default=None)
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
