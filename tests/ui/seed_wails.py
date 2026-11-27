"""Create disposable, synthetic library data for native UI verification."""
import json
import pathlib
import sqlite3
import struct
import sys
import time
import zlib

root = pathlib.Path(sys.argv[1]).resolve()
repo = pathlib.Path(__file__).resolve().parents[2]
allowed = repo / ".test-data" / "ui-verification"
if not root.is_relative_to(allowed) or root == allowed:
    raise SystemExit("UI fixture must be inside .test-data/ui-verification/<run>")
root.mkdir(parents=True, exist_ok=False)
(root / "data").mkdir()
(root / "fixture-media").mkdir()

def chunk(kind, data):
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

def image_bytes(index):
    width, height = 480, 300
    rows = bytearray()
    for y in range(height):
        rows.append(0)
        for x in range(width):
            rows.extend(((x * 255 // width + index * 45) % 256, y * 255 // height, 140 + index * 30))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")

with sqlite3.connect(root / "asterodia.db") as db:
    db.executescript((repo / "internal/database/schema.sql").read_text(encoding="utf-8"))
    db.execute("PRAGMA user_version=3")
    now = int(time.time() * 1000)
    for index, title in enumerate(("검증용 이미지 #1", "검색용 풍경 이미지"), start=1):
        path = root / "fixture-media" / f"테스트 #{index}%.png"
        path.write_bytes(image_bytes(index))
        db.execute("INSERT INTO media(id,source_kind,source_title,created_at,updated_at) VALUES(?,'legacy',?,?,?)", (index, title, now + index, now + index))
        db.execute("INSERT INTO media_file(media_id,file_key,storage_kind,title,filepath,kind,position,filesize,created_at) VALUES(?,?,'unverified_legacy',?,?,'image',0,?,?)", (index, f"ui:{index}", title, str(path), path.stat().st_size, now))
    db.execute("INSERT INTO tag(name) VALUES('검증')")
    db.execute("INSERT INTO media_tag(media_id,tag_id) VALUES(1,1)")
    db.execute("INSERT INTO favorite(media_id,created_at) VALUES(1,?)", (now,))
    if list(db.execute("PRAGMA foreign_key_check")):
        raise SystemExit("Invalid fixture relationships")
print(root)
