#!/bin/sh
#
# gen-backlog.sh — Generate backlog.html from live bd CLI data
#
# Requires: bd, python3
# Usage: ./scripts/gen-backlog.sh [output-file]
#
set -e

OUTPUT="${1:-backlog.html}"

if ! command -v bd >/dev/null 2>&1; then
    echo "Error: bd CLI not found" >&2
    exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
    echo "Error: python3 not found" >&2
    exit 1
fi

# Fetch all open issues into a temp file
TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT
bd list --status open --json 2>/dev/null > "$TMPFILE" || {
    echo "Error: bd list failed" >&2
    exit 1
}

# Use Python to merge epic children and generate HTML
python3 - "$OUTPUT" "$TMPFILE" <<'PYEOF'
import json, sys, subprocess, html
from datetime import datetime, timezone

output_path = sys.argv[1]
with open(sys.argv[2]) as f:
    issues = json.load(f)

# Identify epics and fetch their children (which may include closed items)
epics = {}
standalone = []
child_ids = set()

for issue in issues:
    if issue.get("issue_type") == "epic":
        epic_id = issue["id"]
        try:
            result = subprocess.run(
                ["bd", "children", epic_id, "--json"],
                capture_output=True, text=True, timeout=10
            )
            children = json.loads(result.stdout) if result.returncode == 0 else []
        except Exception:
            children = []
        epics[epic_id] = {**issue, "children": children}
        for c in children:
            child_ids.add(c["id"])
    else:
        # Collect non-epics; filter out children later
        standalone.append(issue)

# Remove items that belong to an epic from standalone list
standalone = [s for s in standalone if s["id"] not in child_ids]

# Sort epics by priority then title
sorted_epics = sorted(epics.values(), key=lambda e: (e.get("priority", 99), e.get("title", "")))
# Sort standalone by priority then title
standalone.sort(key=lambda s: (s.get("priority", 99), s.get("title", "")))

# Compute stats
total_open = len(issues)
priority_counts = {}
for issue in issues:
    p = issue.get("priority", 0)
    priority_counts[p] = priority_counts.get(p, 0) + 1
epic_count = len(epics)

now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M UTC")

def esc(s):
    return html.escape(str(s)) if s else ""

def priority_class(p):
    return f"p{p}" if p in (1, 2, 3, 4) else "p4"

def status_badge(status):
    cls = "open" if status == "open" else "closed"
    return f'<span class="status-badge {cls}">{esc(status)}</span>'

def priority_badge(p):
    return f'<span class="priority-badge {priority_class(p)}">P{p}</span>'

# Build the data blob for JS
all_data = {
    "generated": now,
    "epics": sorted_epics,
    "standalone": standalone,
    "stats": {
        "total_open": total_open,
        "priority_counts": priority_counts,
        "epic_count": epic_count,
    },
}

data_json = json.dumps(all_data, indent=None)

html_content = f'''<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Stringer Backlog</title>
<style>
* {{ margin: 0; padding: 0; box-sizing: border-box; }}
body {{
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
  background: #0d1117;
  color: #e6edf3;
  min-height: 100vh;
}}

header {{
  padding: 24px 32px 16px;
  border-bottom: 1px solid #21262d;
  display: flex;
  align-items: baseline;
  gap: 16px;
  flex-wrap: wrap;
}}
header h1 {{
  font-size: 22px;
  font-weight: 600;
  color: #f0f6fc;
  letter-spacing: -0.3px;
}}
header .stats {{
  font-size: 13px;
  color: #7d8590;
  display: flex;
  gap: 16px;
}}
header .stats span {{ display: flex; align-items: center; gap: 4px; }}
header .stats .dot {{
  width: 8px; height: 8px; border-radius: 50%; display: inline-block;
}}
.generated {{
  font-size: 11px;
  color: #484f58;
  margin-left: auto;
}}

.controls {{
  padding: 12px 32px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  border-bottom: 1px solid #21262d;
}}
.controls button {{
  background: #21262d;
  border: 1px solid #30363d;
  color: #e6edf3;
  padding: 5px 12px;
  border-radius: 6px;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
}}
.controls button:hover {{ background: #30363d; border-color: #484f58; }}
.controls button.active {{
  background: #1f6feb22;
  border-color: #1f6feb;
  color: #58a6ff;
}}
.controls .sep {{
  width: 1px;
  background: #30363d;
  margin: 0 4px;
}}

.board {{
  padding: 20px 32px;
  max-width: 960px;
}}

.section-header {{
  font-size: 13px;
  font-weight: 600;
  color: #7d8590;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 24px 0 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid #21262d;
}}

.epic-row {{
  background: #161b22;
  border: 1px solid #30363d;
  border-radius: 8px;
  margin-bottom: 8px;
  overflow: hidden;
}}
.epic-header {{
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}}
.epic-header:hover {{ background: #1c2129; }}
.epic-chevron {{
  color: #484f58;
  font-size: 12px;
  transition: transform 0.2s;
  width: 16px;
  text-align: center;
  flex-shrink: 0;
}}
.epic-row.expanded .epic-chevron {{ transform: rotate(90deg); }}
.epic-title {{
  flex: 1;
  font-size: 14px;
  font-weight: 500;
}}
.epic-id {{
  font-family: monospace;
  font-size: 11px;
  color: #7d8590;
}}
.child-count {{
  font-size: 11px;
  color: #7d8590;
  background: #21262d;
  padding: 2px 8px;
  border-radius: 10px;
}}

.children {{
  display: none;
  border-top: 1px solid #21262d;
}}
.epic-row.expanded .children {{ display: block; }}

.child-row {{
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px 10px 42px;
  border-bottom: 1px solid #21262d;
  cursor: pointer;
  transition: background 0.15s;
}}
.child-row:last-child {{ border-bottom: none; }}
.child-row:hover {{ background: #1c2129; }}
.child-id {{
  font-family: monospace;
  font-size: 11px;
  color: #7d8590;
  flex-shrink: 0;
  width: 120px;
}}
.child-title {{
  flex: 1;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}}
.child-desc {{
  font-size: 11px;
  color: #7d8590;
  max-width: 300px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}}

.task-row {{
  background: #161b22;
  border: 1px solid #30363d;
  border-radius: 8px;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  cursor: pointer;
  transition: background 0.15s;
}}
.task-row:hover {{ background: #1c2129; }}
.task-id {{
  font-family: monospace;
  font-size: 11px;
  color: #7d8590;
  flex-shrink: 0;
  width: 120px;
}}
.task-title {{
  flex: 1;
  font-size: 14px;
  font-weight: 500;
}}

.priority-badge {{
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 10px;
  letter-spacing: 0.3px;
  flex-shrink: 0;
}}
.priority-badge.p1 {{ background: #8b5cf6; color: #fff; }}
.priority-badge.p2 {{ background: #da3633; color: #fff; }}
.priority-badge.p3 {{ background: #d29922; color: #fff; }}
.priority-badge.p4 {{ background: #30363d; color: #8b949e; }}

.status-badge {{
  font-size: 10px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 10px;
  text-transform: uppercase;
  letter-spacing: 0.3px;
  flex-shrink: 0;
}}
.status-badge.open {{ background: #238636; color: #fff; }}
.status-badge.closed {{ background: #8b949e33; color: #8b949e; }}

/* Modal */
.modal-overlay {{
  display: none;
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.6);
  z-index: 100;
  align-items: center;
  justify-content: center;
}}
.modal-overlay.visible {{ display: flex; }}
.modal {{
  background: #161b22;
  border: 1px solid #30363d;
  border-radius: 12px;
  padding: 24px;
  max-width: 640px;
  width: 90%;
  max-height: 80vh;
  overflow-y: auto;
  position: relative;
}}
.modal-close {{
  position: absolute;
  top: 12px;
  right: 16px;
  background: none;
  border: none;
  color: #7d8590;
  font-size: 20px;
  cursor: pointer;
}}
.modal-close:hover {{ color: #e6edf3; }}
.modal h2 {{
  font-size: 16px;
  font-weight: 600;
  margin-bottom: 4px;
  padding-right: 32px;
}}
.modal .modal-id {{
  font-family: monospace;
  font-size: 12px;
  color: #7d8590;
  margin-bottom: 16px;
}}
.modal .modal-meta {{
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}}
.modal .modal-body {{
  font-size: 13px;
  line-height: 1.6;
  color: #c9d1d9;
  white-space: pre-wrap;
  word-break: break-word;
}}

.empty-state {{
  text-align: center;
  color: #484f58;
  padding: 48px 0;
  font-size: 14px;
}}
</style>
</head>
<body>

<header>
  <h1>Stringer Backlog</h1>
  <div class="stats" id="stats"></div>
  <span class="generated">Generated: {now}</span>
</header>

<div class="controls" id="controls"></div>

<div class="board" id="board"></div>

<div class="modal-overlay" id="modal-overlay">
  <div class="modal" id="modal">
    <button class="modal-close" id="modal-close">&times;</button>
    <h2 id="modal-title"></h2>
    <div class="modal-id" id="modal-id"></div>
    <div class="modal-meta" id="modal-meta"></div>
    <div class="modal-body" id="modal-body"></div>
  </div>
</div>

<script>
const DATA = {data_json};

// State
let priorityFilter = "all";
let statusFilter = "all";

function esc(s) {{
  const d = document.createElement("div");
  d.textContent = s || "";
  return d.innerHTML;
}}

function renderStats() {{
  const s = DATA.stats;
  const dots = {{1: "#8b5cf6", 2: "#da3633", 3: "#d29922", 4: "#484f58"}};
  let html = `<span><span class="dot" style="background:#238636"></span>${{s.total_open}} open</span>`;
  html += `<span>${{s.epic_count}} epics</span>`;
  for (const p of [1,2,3,4]) {{
    const c = s.priority_counts[p] || 0;
    if (c > 0) html += `<span><span class="dot" style="background:${{dots[p]}}"></span>P${{p}}: ${{c}}</span>`;
  }}
  document.getElementById("stats").innerHTML = html;
}}

function renderControls() {{
  const prios = [1,2,3,4].filter(p => DATA.stats.priority_counts[p]);
  let html = `<button class="${{priorityFilter==='all'?'active':''}}" data-prio="all">All</button>`;
  for (const p of prios) {{
    html += `<button class="${{priorityFilter===String(p)?'active':''}}" data-prio="${{p}}">P${{p}}</button>`;
  }}
  html += `<div class="sep"></div>`;
  html += `<button class="${{statusFilter==='all'?'active':''}}" data-status="all">All status</button>`;
  html += `<button class="${{statusFilter==='open'?'active':''}}" data-status="open">Open</button>`;
  html += `<button class="${{statusFilter==='closed'?'active':''}}" data-status="closed">Closed</button>`;
  const el = document.getElementById("controls");
  el.innerHTML = html;
  el.querySelectorAll("[data-prio]").forEach(b => b.addEventListener("click", () => {{
    priorityFilter = b.dataset.prio;
    render();
  }}));
  el.querySelectorAll("[data-status]").forEach(b => b.addEventListener("click", () => {{
    statusFilter = b.dataset.status;
    render();
  }}));
}}

function matchPriority(item) {{
  if (priorityFilter === "all") return true;
  return String(item.priority) === priorityFilter;
}}

function matchStatus(item) {{
  if (statusFilter === "all") return true;
  return item.status === statusFilter;
}}

function filterChildren(children) {{
  return children.filter(c => matchStatus(c));
}}

function renderBoard() {{
  let html = "";

  // Epics
  const epics = DATA.epics.filter(matchPriority);
  if (epics.length > 0) {{
    html += `<div class="section-header">Epics</div>`;
    for (const epic of epics) {{
      const children = filterChildren(epic.children || []);
      const totalChildren = (epic.children || []).length;
      const openChildren = (epic.children || []).filter(c => c.status === "open").length;
      const closedChildren = totalChildren - openChildren;
      const childCountLabel = statusFilter === "all"
        ? `${{openChildren}} open / ${{closedChildren}} closed`
        : `${{children.length}} ${{statusFilter}}`;
      html += `<div class="epic-row" data-epic="${{esc(epic.id)}}">`;
      html += `<div class="epic-header">`;
      html += `<span class="epic-chevron">&#9654;</span>`;
      html += `<span class="priority-badge p${{epic.priority || 4}}">P${{epic.priority || 4}}</span>`;
      html += `<span class="epic-title">${{esc(epic.title)}}</span>`;
      html += `<span class="epic-id">${{esc(epic.id)}}</span>`;
      html += `<span class="child-count">${{childCountLabel}}</span>`;
      html += `</div>`;
      html += `<div class="children">`;
      if (children.length === 0) {{
        html += `<div class="child-row"><span style="color:#484f58;font-size:12px">No ${{statusFilter === "all" ? "" : statusFilter + " "}}children</span></div>`;
      }}
      for (const child of children) {{
        const desc = (child.description || "").substring(0, 120);
        html += `<div class="child-row" data-issue='${{JSON.stringify(child).replace(/'/g, "&#39;")}}'>`;
        html += `<span class="child-id">${{esc(child.id)}}</span>`;
        html += `<span class="status-badge ${{child.status === "open" ? "open" : "closed"}}">${{esc(child.status)}}</span>`;
        html += `<span class="priority-badge p${{child.priority || 4}}">P${{child.priority || 4}}</span>`;
        html += `<span class="child-title">${{esc(child.title)}}</span>`;
        html += `<span class="child-desc">${{esc(desc)}}</span>`;
        html += `</div>`;
      }}
      html += `</div></div>`;
    }}
  }}

  // Standalone tasks
  const tasks = DATA.standalone.filter(t => matchPriority(t) && matchStatus(t));
  if (tasks.length > 0) {{
    html += `<div class="section-header">Tasks</div>`;
    for (const task of tasks) {{
      html += `<div class="task-row" data-issue='${{JSON.stringify(task).replace(/'/g, "&#39;")}}'>`;
      html += `<span class="task-id">${{esc(task.id)}}</span>`;
      html += `<span class="status-badge ${{task.status === "open" ? "open" : "closed"}}">${{esc(task.status)}}</span>`;
      html += `<span class="priority-badge p${{task.priority || 4}}">P${{task.priority || 4}}</span>`;
      html += `<span class="task-title">${{esc(task.title)}}</span>`;
      html += `</div>`;
    }}
  }}

  if (epics.length === 0 && tasks.length === 0) {{
    html += `<div class="empty-state">No issues match the current filters.</div>`;
  }}

  document.getElementById("board").innerHTML = html;

  // Epic accordion
  document.querySelectorAll(".epic-header").forEach(h => {{
    h.addEventListener("click", () => {{
      h.parentElement.classList.toggle("expanded");
    }});
  }});

  // Child/task click -> modal
  document.querySelectorAll("[data-issue]").forEach(el => {{
    el.addEventListener("click", (e) => {{
      e.stopPropagation();
      const issue = JSON.parse(el.dataset.issue);
      showModal(issue);
    }});
  }});
}}

function showModal(issue) {{
  document.getElementById("modal-title").textContent = issue.title || "";
  document.getElementById("modal-id").textContent = issue.id || "";
  let meta = `<span class="priority-badge p${{issue.priority || 4}}">P${{issue.priority || 4}}</span>`;
  meta += `<span class="status-badge ${{issue.status === "open" ? "open" : "closed"}}">${{issue.status || ""}}</span>`;
  if (issue.issue_type) meta += `<span style="font-size:11px;color:#7d8590">${{esc(issue.issue_type)}}</span>`;
  document.getElementById("modal-meta").innerHTML = meta;
  document.getElementById("modal-body").textContent = issue.description || "(no description)";
  document.getElementById("modal-overlay").classList.add("visible");
}}

document.getElementById("modal-close").addEventListener("click", () => {{
  document.getElementById("modal-overlay").classList.remove("visible");
}});
document.getElementById("modal-overlay").addEventListener("click", (e) => {{
  if (e.target === e.currentTarget) {{
    e.currentTarget.classList.remove("visible");
  }}
}});
document.addEventListener("keydown", (e) => {{
  if (e.key === "Escape") document.getElementById("modal-overlay").classList.remove("visible");
}});

function render() {{
  renderStats();
  renderControls();
  renderBoard();
}}

render();
</script>
</body>
</html>'''

with open(output_path, "w") as f:
    f.write(html_content)

print(f"Generated {output_path} ({len(issues)} open issues, {len(epics)} epics)")
PYEOF
