"""SQLite-backed request store (stands in for platform Postgres — DESIGN.md §10).

Request inputs live here, keyed by request_id. The workflow carries only the
request_id and reads inputs via the load_inputs activity.
"""
import json
import sqlite3
from pathlib import Path

DB_PATH = Path(__file__).resolve().parents[2] / "waas.db"


def _connect() -> sqlite3.Connection:
    return sqlite3.connect(DB_PATH)


def init_db() -> None:
    with _connect() as conn:
        conn.execute(
            "CREATE TABLE IF NOT EXISTS requests ("
            " request_id TEXT PRIMARY KEY,"
            " catalog_item TEXT,"
            " inputs_json TEXT NOT NULL)"
        )


def create(request_id: str, catalog_item: str, inputs: dict) -> None:
    with _connect() as conn:
        conn.execute(
            "INSERT INTO requests (request_id, catalog_item, inputs_json) VALUES (?, ?, ?)",
            (request_id, catalog_item, json.dumps(inputs)),
        )


def get_inputs(request_id: str) -> dict | None:
    with _connect() as conn:
        row = conn.execute(
            "SELECT inputs_json FROM requests WHERE request_id = ?", (request_id,)
        ).fetchone()
    return json.loads(row[0]) if row else None


def update_inputs(request_id: str, inputs: dict) -> None:
    with _connect() as conn:
        conn.execute(
            "UPDATE requests SET inputs_json = ? WHERE request_id = ?",
            (json.dumps(inputs), request_id),
        )
