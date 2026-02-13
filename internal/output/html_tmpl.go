package output

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Stringer Dashboard</title>
<style>
:root {
  --bg: #fff; --fg: #1a1a2e; --card-bg: #f8f9fa; --border: #dee2e6;
  --table-alt: #f1f3f5; --hover: #e9ecef; --muted: #6c757d;
  --p1: #dc3545; --p2: #fd7e14; --p3: #ffc107; --p4: #28a745;
  --accent: #0d6efd;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #1a1a2e; --fg: #e9ecef; --card-bg: #16213e; --border: #495057;
    --table-alt: #0f3460; --hover: #1a1a4e; --muted: #adb5bd;
    --p1: #f55; --p2: #fd7e14; --p3: #ffc107; --p4: #4caf50;
    --accent: #5b9aff;
  }
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--fg); line-height: 1.5; padding: 1rem; max-width: 1400px; margin: 0 auto; }
header { margin-bottom: 1.5rem; }
header h1 { font-size: 1.5rem; margin-bottom: .25rem; }
header p { color: var(--muted); font-size: .875rem; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: .75rem; margin-bottom: 1.5rem; }
.card { background: var(--card-bg); border: 1px solid var(--border); border-radius: 8px; padding: .75rem; text-align: center; }
.card .value { font-size: 1.5rem; font-weight: 700; }
.card .label { font-size: .75rem; color: var(--muted); text-transform: uppercase; }
.card-p1 .value { color: var(--p1); }
.card-p2 .value { color: var(--p2); }
.card-p3 .value { color: var(--p3); }
.card-p4 .value { color: var(--p4); }
.charts { display: grid; grid-template-columns: repeat(2, 1fr); gap: 1rem; margin-bottom: 1.5rem; }
@media (max-width: 768px) { .charts { grid-template-columns: 1fr; } }
.chart-box { background: var(--card-bg); border: 1px solid var(--border); border-radius: 8px; padding: 1rem; }
.chart-box h3 { font-size: .875rem; margin-bottom: .5rem; }
.filters { display: flex; flex-wrap: wrap; gap: .5rem; margin-bottom: 1rem; align-items: center; }
.filters select, .filters input { padding: .375rem .5rem; border: 1px solid var(--border); border-radius: 4px; background: var(--card-bg); color: var(--fg); font-size: .8125rem; }
.filters input[type=text] { min-width: 180px; }
table { width: 100%; border-collapse: collapse; font-size: .8125rem; }
thead { position: sticky; top: 0; background: var(--card-bg); }
th, td { padding: .5rem .625rem; text-align: left; border-bottom: 1px solid var(--border); }
th { cursor: pointer; user-select: none; white-space: nowrap; }
th:hover { color: var(--accent); }
tr:nth-child(even) { background: var(--table-alt); }
tr:hover { background: var(--hover); }
tr.signal-row { cursor: pointer; }
tr.detail-row td { padding: .75rem 1rem; font-size: .8125rem; color: var(--muted); white-space: pre-wrap; }
.hidden { display: none; }
.priority { font-weight: 700; padding: .125rem .375rem; border-radius: 3px; font-size: .75rem; }
.priority-1 { color: var(--p1); }
.priority-2 { color: var(--p2); }
.priority-3 { color: var(--p3); }
.priority-4 { color: var(--p4); }
.sort-arrow { font-size: .625rem; margin-left: .25rem; }
</style>
</head>
<body>
<header>
  <h1>Stringer Dashboard</h1>
  <p>Generated {{.GeneratedAt}} &middot; {{.TotalSignals}} signals from {{len .Collectors}} collector(s)</p>
</header>

<section class="cards" id="summary">
  <div class="card"><div class="value">{{.TotalSignals}}</div><div class="label">Total</div></div>
  <div class="card card-p1"><div class="value">{{index .PriorityDist 0}}</div><div class="label">P1 Critical</div></div>
  <div class="card card-p2"><div class="value">{{index .PriorityDist 1}}</div><div class="label">P2 High</div></div>
  <div class="card card-p3"><div class="value">{{index .PriorityDist 2}}</div><div class="label">P3 Medium</div></div>
  <div class="card card-p4"><div class="value">{{index .PriorityDist 3}}</div><div class="label">P4 Low</div></div>
</section>

<section class="charts" id="charts">
  <div class="chart-box"><h3>Priority Distribution</h3><div id="chart-priority"></div></div>
  <div class="chart-box"><h3>Signal Sources</h3><div id="chart-sources"></div></div>
  {{if .ChurnFiles}}<div class="chart-box"><h3>Top File Churn</h3><div id="chart-churn"></div></div>{{end}}
  {{if .LotteryRisk}}<div class="chart-box"><h3>Lottery Risk</h3><div id="chart-lottery"></div></div>{{end}}
  {{if .TodoAgeBuckets}}<div class="chart-box"><h3>TODO Age</h3><div id="chart-todo-age"></div></div>{{end}}
</section>

<section id="filters" class="filters">
  <select id="filter-collector" onchange="applyFilters()">
    <option value="">All Collectors</option>
    {{range .Collectors}}<option value="{{.}}">{{.}}</option>{{end}}
  </select>
  <select id="filter-priority" onchange="applyFilters()">
    <option value="">All Priorities</option>
    <option value="1">P1</option><option value="2">P2</option>
    <option value="3">P3</option><option value="4">P4</option>
  </select>
  <input type="range" id="filter-confidence" min="0" max="100" value="0" oninput="applyFilters();this.title='Min confidence: '+(this.value/100).toFixed(2)">
  <input type="text" id="filter-search" placeholder="Search..." oninput="applyFilters()">
</section>

<section id="signals">
<table>
<thead><tr>
  <th data-col="title">Title</th>
  <th data-col="kind">Kind</th>
  <th data-col="source">Source</th>
  <th data-col="location">Location</th>
  <th data-col="confidence">Confidence</th>
  <th data-col="priority">Priority</th>
</tr></thead>
<tbody>
{{range .SignalRows}}
<tr class="signal-row" data-source="{{.Source}}" data-priority="{{.Priority}}" data-confidence="{{.Confidence}}" onclick="toggleDetail(this)">
  <td>{{.Title}}</td><td>{{.Kind}}</td><td>{{.Source}}</td><td>{{.Location}}</td>
  <td>{{printf "%.2f" .Confidence}}</td>
  <td><span class="priority priority-{{.Priority}}">P{{.Priority}}</span></td>
</tr>
<tr class="detail-row hidden"><td colspan="6">{{.Description}}</td></tr>
{{end}}
</tbody>
</table>
</section>

<script>
var chartData = {{json .ChartData}};

function svgEl(tag, attrs) {
  var el = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (var k in attrs) el.setAttribute(k, attrs[k]);
  return el;
}

function renderBarChart(id, labels, values, colors) {
  var c = document.getElementById(id); if (!c) return;
  var max = Math.max.apply(null, values) || 1;
  var h = labels.length * 28 + 4;
  var svg = svgEl("svg", {width:"100%", viewBox:"0 0 400 "+h});
  for (var i = 0; i < labels.length; i++) {
    var w = (values[i]/max)*280;
    var y = i*28+2;
    svg.appendChild(svgEl("rect", {x:110, y:y, width:Math.max(w,2), height:20, fill:colors[i%colors.length], rx:3}));
    var txt = svgEl("text", {x:105, y:y+14, "text-anchor":"end", fill:"currentColor", "font-size":"11"});
    txt.textContent = labels[i].length > 18 ? labels[i].slice(0,16)+"..." : labels[i];
    svg.appendChild(txt);
    var val = svgEl("text", {x:115+w, y:y+14, fill:"currentColor", "font-size":"11"});
    val.textContent = values[i];
    svg.appendChild(val);
  }
  c.appendChild(svg);
}

function renderDoughnut(id, labels, values, colors) {
  var c = document.getElementById(id); if (!c) return;
  var total = values.reduce(function(a,b){return a+b},0);
  if (!total) return;
  var svg = svgEl("svg", {width:"100%", viewBox:"0 0 300 160"});
  var cx=80, cy=80, r=60, angle=-Math.PI/2;
  for (var i = 0; i < values.length; i++) {
    var slice = (values[i]/total)*Math.PI*2;
    if (values[i] === 0) continue;
    var x1=cx+r*Math.cos(angle), y1=cy+r*Math.sin(angle);
    angle += slice;
    var x2=cx+r*Math.cos(angle), y2=cy+r*Math.sin(angle);
    var large = slice > Math.PI ? 1 : 0;
    var d = "M"+cx+","+cy+" L"+x1+","+y1+" A"+r+","+r+" 0 "+large+",1 "+x2+","+y2+" Z";
    svg.appendChild(svgEl("path", {d:d, fill:colors[i%colors.length]}));
  }
  svg.appendChild(svgEl("circle", {cx:cx, cy:cy, r:30, fill:"var(--card-bg)"}));
  for (var j = 0; j < labels.length; j++) {
    if (values[j] === 0) continue;
    var ly = 16 + j*18;
    svg.appendChild(svgEl("rect", {x:175, y:ly-8, width:10, height:10, fill:colors[j%colors.length], rx:2}));
    var lt = svgEl("text", {x:190, y:ly+1, fill:"currentColor", "font-size":"11"});
    lt.textContent = labels[j]+" ("+values[j]+")";
    svg.appendChild(lt);
  }
  c.appendChild(svg);
}

(function(){
  var pc = ["var(--p1)","var(--p2)","var(--p3)","var(--p4)"];
  renderBarChart("chart-priority", ["P1","P2","P3","P4"], chartData.priority, pc);
  renderDoughnut("chart-sources", chartData.sourceLabels, chartData.sourceValues,
    ["#0d6efd","#6f42c1","#20c997","#fd7e14","#e83e8c","#17a2b8","#6c757d","#28a745"]);
  if (chartData.churnLabels) renderBarChart("chart-churn", chartData.churnLabels, chartData.churnValues, ["var(--accent)"]);
  if (chartData.lotteryLabels) renderBarChart("chart-lottery", chartData.lotteryLabels, chartData.lotteryValues, ["var(--p2)"]);
  if (chartData.todoAgeLabels) renderBarChart("chart-todo-age", chartData.todoAgeLabels, chartData.todoAgeValues, ["var(--p3)"]);
})();

function applyFilters() {
  var col = document.getElementById("filter-collector").value;
  var pri = document.getElementById("filter-priority").value;
  var conf = document.getElementById("filter-confidence").value / 100;
  var search = document.getElementById("filter-search").value.toLowerCase();
  var rows = document.querySelectorAll("tr.signal-row");
  for (var i = 0; i < rows.length; i++) {
    var r = rows[i];
    var show = true;
    if (col && r.dataset.source !== col) show = false;
    if (pri && r.dataset.priority !== pri) show = false;
    if (parseFloat(r.dataset.confidence) < conf) show = false;
    if (search && r.textContent.toLowerCase().indexOf(search) === -1) show = false;
    r.classList.toggle("hidden", !show);
    r.nextElementSibling.classList.add("hidden");
  }
}

function toggleDetail(row) { row.nextElementSibling.classList.toggle("hidden"); }

(function(){
  var headers = document.querySelectorAll("th[data-col]");
  var sortCol = "", sortAsc = true;
  for (var i = 0; i < headers.length; i++) {
    headers[i].addEventListener("click", (function(th){
      return function(){
        var col = th.dataset.col;
        if (sortCol === col) sortAsc = !sortAsc; else { sortCol = col; sortAsc = true; }
        var tbody = document.querySelector("tbody");
        var pairs = [];
        var srows = tbody.querySelectorAll("tr.signal-row");
        for (var j = 0; j < srows.length; j++) {
          pairs.push([srows[j], srows[j].nextElementSibling]);
        }
        var ci = Array.prototype.indexOf.call(th.parentNode.children, th);
        pairs.sort(function(a,b){
          var av = a[0].children[ci].textContent, bv = b[0].children[ci].textContent;
          var an = parseFloat(av), bn = parseFloat(bv);
          if (!isNaN(an) && !isNaN(bn)) return sortAsc ? an-bn : bn-an;
          return sortAsc ? av.localeCompare(bv) : bv.localeCompare(av);
        });
        for (var k = 0; k < pairs.length; k++) {
          tbody.appendChild(pairs[k][0]);
          tbody.appendChild(pairs[k][1]);
        }
        document.querySelectorAll(".sort-arrow").forEach(function(e){e.remove();});
        var arrow = document.createElement("span");
        arrow.className = "sort-arrow";
        arrow.textContent = sortAsc ? " \u25B2" : " \u25BC";
        th.appendChild(arrow);
      };
    })(headers[i]));
  }
})();
</script>
</body>
</html>`
