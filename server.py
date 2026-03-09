import json
import os
import time
import shutil
from datetime import datetime
from typing import Any, Dict, List, Optional

from flask import Flask, send_from_directory, request, jsonify
from filelock import FileLock

BASE_DIR = os.path.abspath(os.path.dirname(__file__))
APP_DIR = os.path.join(BASE_DIR, "app")
DATA_DIR = os.path.join(APP_DIR, "data")

INVENTORY_PATH = os.path.join(DATA_DIR, "inventory.json")
LOCK_PATH = INVENTORY_PATH + ".lock"

BACKUP_DIR = os.path.join(DATA_DIR, "backups")
MAX_BACKUPS = 100

HOURLY_BACKUP_DIR = os.path.join(DATA_DIR, "hourly_backups")
MAX_HOURLY_BACKUPS = 48

ADMIN_USERNAME = os.environ.get("LOGAPP_ADMIN_USERNAME")
ADMIN_PASSWORD = os.environ.get("LOGAPP_ADMIN_PASSWORD")

EDITOR_LOCK: Dict[str, Any] = {
    "token": None,
    "expires_at": 0.0,
    "mode": None,  # "admin" or "normal"
}

EDITOR_TTL_SECONDS = 15 * 60  # 15 minutes
PORT = int(os.environ.get("PORT", "8787"))

app = Flask(__name__, static_folder=None)


def _ensure_files() -> None:
    os.makedirs(DATA_DIR, exist_ok=True)
    os.makedirs(BACKUP_DIR, exist_ok=True)
    os.makedirs(HOURLY_BACKUP_DIR, exist_ok=True)

    if not os.path.exists(INVENTORY_PATH):
        with open(INVENTORY_PATH, "w", encoding="utf-8") as f:
            json.dump([], f, ensure_ascii=False, indent=2)


def _cleanup_old_backups() -> None:
    if not os.path.exists(BACKUP_DIR):
        return

    files = [
        os.path.join(BACKUP_DIR, name)
        for name in os.listdir(BACKUP_DIR)
        if name.lower().endswith(".json")
    ]

    files.sort(key=os.path.getmtime, reverse=True)

    for old_file in files[MAX_BACKUPS:]:
        try:
            os.remove(old_file)
        except OSError:
            pass


def _create_backup() -> None:
    if not os.path.exists(INVENTORY_PATH):
        return

    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    backup_name = f"inventory_{timestamp}.json"
    backup_path = os.path.join(BACKUP_DIR, backup_name)

    shutil.copy2(INVENTORY_PATH, backup_path)
    _cleanup_old_backups()


def _cleanup_old_hourly_backups() -> None:
    if not os.path.exists(HOURLY_BACKUP_DIR):
        return

    files = [
        os.path.join(HOURLY_BACKUP_DIR, name)
        for name in os.listdir(HOURLY_BACKUP_DIR)
        if name.lower().endswith(".json")
    ]

    files.sort(key=os.path.getmtime, reverse=True)

    for old_file in files[MAX_HOURLY_BACKUPS:]:
        try:
            os.remove(old_file)
        except OSError:
            pass


def _create_hourly_backup_if_needed() -> None:
    if not os.path.exists(INVENTORY_PATH):
        return

    current_hour_stamp = datetime.now().strftime("%Y%m%d_%H")
    backup_name = f"inventory_hourly_{current_hour_stamp}.json"
    backup_path = os.path.join(HOURLY_BACKUP_DIR, backup_name)

    if not os.path.exists(backup_path):
        shutil.copy2(INVENTORY_PATH, backup_path)
        _cleanup_old_hourly_backups()


def _read_inventory() -> List[Dict[str, Any]]:
    _ensure_files()
    with FileLock(LOCK_PATH, timeout=5):
        with open(INVENTORY_PATH, "r", encoding="utf-8") as f:
            return json.load(f)


def _write_inventory(data: List[Dict[str, Any]]) -> None:
    _ensure_files()
    with FileLock(LOCK_PATH, timeout=5):
        _create_backup()
        _create_hourly_backup_if_needed()

        tmp = INVENTORY_PATH + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
        os.replace(tmp, INVENTORY_PATH)


def _lock_is_active() -> bool:
    return (
        EDITOR_LOCK["token"] is not None
        and time.time() < float(EDITOR_LOCK["expires_at"])
    )


def _lock_is_owned(token: Optional[str]) -> bool:
    return _lock_is_active() and token is not None and token == EDITOR_LOCK["token"]


def _clear_lock() -> None:
    EDITOR_LOCK["token"] = None
    EDITOR_LOCK["expires_at"] = 0.0
    EDITOR_LOCK["mode"] = None


@app.route("/")
def root():
    return send_from_directory(APP_DIR, "index.html")


@app.route("/resources/<path:filename>")
def resources(filename: str):
    return send_from_directory(os.path.join(APP_DIR, "resources"), filename)


@app.route("/api/admin-auth", methods=["POST"])
def api_admin_auth():
    if not ADMIN_USERNAME or not ADMIN_PASSWORD:
        return jsonify({
            "ok": False,
            "error": "admin_credentials_not_configured"
        }), 500

    body = request.get_json(silent=True) or {}
    username = str(body.get("username") or "").strip()
    password = str(body.get("password") or "")

    if username == ADMIN_USERNAME and password == ADMIN_PASSWORD:
        return jsonify({"ok": True})

    return jsonify({"ok": False, "error": "invalid_credentials"}), 401


@app.route("/api/inventory", methods=["GET"])
def api_get_inventory():
    data = _read_inventory()
    return jsonify({
        "items": data,
        "lock": {
            "active": _lock_is_active(),
            "expires_at": EDITOR_LOCK["expires_at"] if _lock_is_active() else None,
            "mode": EDITOR_LOCK["mode"] if _lock_is_active() else None,
        }
    })


@app.route("/api/lock", methods=["POST"])
def api_lock_acquire():
    body = request.get_json(silent=True) or {}
    token = str(body.get("token") or "").strip()[:120]
    mode = str(body.get("mode") or "normal").strip().lower()

    if mode not in ("admin", "normal"):
        mode = "normal"

    if not token:
        return jsonify({"ok": False, "error": "token_required"}), 400

    if not _lock_is_active():
        _clear_lock()

    if _lock_is_active() and EDITOR_LOCK["token"] != token:
        return jsonify({
            "ok": False,
            "error": "locked",
            "expires_at": EDITOR_LOCK["expires_at"],
            "mode": EDITOR_LOCK["mode"],
        }), 423

    EDITOR_LOCK["token"] = token
    EDITOR_LOCK["expires_at"] = time.time() + EDITOR_TTL_SECONDS
    EDITOR_LOCK["mode"] = mode

    return jsonify({
        "ok": True,
        "expires_at": EDITOR_LOCK["expires_at"],
        "mode": EDITOR_LOCK["mode"],
    })


@app.route("/api/lock", methods=["DELETE"])
def api_lock_release():
    body = request.get_json(silent=True) or {}
    token = str(body.get("token") or "").strip()[:120]

    if _lock_is_owned(token):
        _clear_lock()
        return jsonify({"ok": True})

    return jsonify({"ok": False, "error": "not_lock_owner"}), 403


@app.route("/api/inventory", methods=["PUT"])
def api_put_inventory():
    body = request.get_json(silent=True) or {}
    token = str(body.get("token") or "").strip()[:120]
    items = body.get("items")

    if not isinstance(items, list):
        return jsonify({"ok": False, "error": "items_must_be_list"}), 400

    if not _lock_is_owned(token):
        return jsonify({
            "ok": False,
            "error": "lock_required",
            "expires_at": EDITOR_LOCK["expires_at"] if _lock_is_active() else None,
            "mode": EDITOR_LOCK["mode"] if _lock_is_active() else None,
        }), 423

    _write_inventory(items)
    EDITOR_LOCK["expires_at"] = time.time() + EDITOR_TTL_SECONDS

    return jsonify({"ok": True})


if __name__ == "__main__":
    _ensure_files()

    if not ADMIN_USERNAME or not ADMIN_PASSWORD:
        raise RuntimeError(
            "Missing required environment variables: "
            "LOGAPP_ADMIN_USERNAME and LOGAPP_ADMIN_PASSWORD"
        )

    app.run(host="0.0.0.0", port=PORT, debug=False, use_reloader=False)
