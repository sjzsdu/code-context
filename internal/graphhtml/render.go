package graphhtml

import (
	"encoding/json"
	"html/template"
	"io"

	"github.com/sjzsdu/code-context/internal/api"
)

type view struct {
	Title     string
	Focus     string
	Summary   string
	NodeCount int
	EdgeCount int
	GraphJSON template.JS
}

func Render(w io.Writer, graph *api.GraphExport) error {
	payload, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	return pageTemplate.Execute(w, view{
		Title:     "code-context graph view",
		Focus:     graph.Focus,
		Summary:   graph.Summary,
		NodeCount: len(graph.Nodes),
		EdgeCount: len(graph.Edges),
		GraphJSON: template.JS(payload),
	})
}

var pageTemplate = template.Must(template.New("graph-html").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #07111f;
      --bg-elevated: rgba(10, 20, 37, 0.92);
      --bg-panel: rgba(8, 17, 31, 0.86);
      --bg-soft: rgba(17, 30, 50, 0.72);
      --line: rgba(113, 149, 196, 0.18);
      --line-strong: rgba(131, 177, 237, 0.36);
      --text: #ecf3ff;
      --muted: #95a7c6;
      --accent: #58d6ff;
      --accent-2: #7c9cff;
      --danger: #ff8f8f;
      --ok: #81f0b3;
      --shadow: 0 30px 80px rgba(0, 0, 0, 0.42);
      --file: #67b7ff;
      --symbol: #ffd166;
      --document: #8ff7c1;
      --module: #ff9ad5;
      --package: #b39cff;
      --import: #ff9671;
    }
    * { box-sizing: border-box; }
    html, body { margin: 0; min-height: 100%; background: radial-gradient(circle at top, rgba(88, 214, 255, 0.15), transparent 30%), radial-gradient(circle at right, rgba(124, 156, 255, 0.18), transparent 35%), var(--bg); color: var(--text); font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { display: flex; flex-direction: column; }
    header { padding: 20px 24px 16px; border-bottom: 1px solid var(--line); background: linear-gradient(180deg, rgba(10,20,37,0.98), rgba(10,20,37,0.82)); backdrop-filter: blur(18px); position: sticky; top: 0; z-index: 20; }
    .hero { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; flex-wrap: wrap; }
    h1 { margin: 0 0 8px; font-size: 28px; letter-spacing: -0.03em; }
    .subtitle { max-width: 840px; color: var(--muted); line-height: 1.45; }
    .hero-pills { display: flex; gap: 10px; flex-wrap: wrap; }
    .pill { border: 1px solid var(--line-strong); background: rgba(17, 30, 50, 0.85); border-radius: 999px; padding: 8px 12px; color: var(--text); font-size: 12px; white-space: nowrap; }
    .layout { display: grid; grid-template-columns: 320px minmax(0, 1fr) 360px; min-height: calc(100vh - 96px); }
    .sidebar, .details { border-right: 1px solid var(--line); background: var(--bg-panel); backdrop-filter: blur(16px); overflow: auto; }
    .details { border-right: 0; border-left: 1px solid var(--line); }
    .content { display: grid; grid-template-rows: auto 1fr auto; min-width: 0; }
    .section { padding: 18px; }
    .card { background: var(--bg-elevated); border: 1px solid var(--line); border-radius: 18px; box-shadow: var(--shadow); padding: 16px; margin-bottom: 16px; }
    .card h2, .card h3 { margin: 0 0 12px; letter-spacing: -0.02em; }
    .muted { color: var(--muted); }
    .grid { display: grid; gap: 12px; }
    .controls-grid { display: grid; gap: 12px; grid-template-columns: repeat(2, minmax(0, 1fr)); }
    label { display: grid; gap: 6px; font-size: 12px; color: var(--muted); }
    input[type="search"], select, button {
      width: 100%; border: 1px solid rgba(122, 161, 219, 0.25); background: rgba(4, 10, 20, 0.9); color: var(--text); border-radius: 12px; padding: 11px 12px; font: inherit;
    }
    button { cursor: pointer; transition: 140ms ease; }
    button:hover { border-color: rgba(88, 214, 255, 0.5); transform: translateY(-1px); }
    .toolbar { display: flex; gap: 10px; flex-wrap: wrap; }
    .toolbar button { width: auto; min-width: 120px; }
    .toggle-row { display: grid; gap: 10px; }
    .toggle { display: flex; align-items: center; gap: 10px; color: var(--text); font-size: 13px; }
    .toggle input { width: 16px; height: 16px; }
    .legend { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
    .legend-item { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 12px; border: 1px solid var(--line); background: rgba(6, 13, 24, 0.72); }
    .swatch { width: 12px; height: 12px; border-radius: 999px; box-shadow: 0 0 18px currentColor; }
    .stage-shell { padding: 18px; min-width: 0; }
    .stage-card { background: linear-gradient(180deg, rgba(11, 22, 39, 0.92), rgba(6, 13, 24, 0.9)); border: 1px solid var(--line); border-radius: 24px; overflow: hidden; box-shadow: var(--shadow); min-height: 720px; display: grid; grid-template-rows: auto 1fr auto; }
    .stage-topbar { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; padding: 16px 18px; border-bottom: 1px solid var(--line); background: rgba(8, 17, 31, 0.78); }
    .stage-metrics { display: flex; gap: 8px; flex-wrap: wrap; }
    .metric { padding: 8px 10px; border-radius: 999px; background: rgba(8, 17, 31, 0.85); border: 1px solid var(--line); color: var(--muted); font-size: 12px; }
    .stage-body { position: relative; min-height: 0; }
    #graphSurface { width: 100%; height: 100%; display: block; min-height: 560px; background: radial-gradient(circle at 20% 20%, rgba(88,214,255,0.07), transparent 22%), radial-gradient(circle at 80% 18%, rgba(255,154,213,0.08), transparent 18%), radial-gradient(circle at 60% 80%, rgba(124,156,255,0.07), transparent 24%), linear-gradient(180deg, rgba(7, 17, 31, 0.96), rgba(4, 10, 20, 0.98)); }
    .stage-hud { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; padding: 14px 18px 18px; border-top: 1px solid var(--line); }
    .chip-row { display: flex; gap: 8px; flex-wrap: wrap; }
    .chip { background: rgba(9, 18, 33, 0.78); border: 1px solid var(--line); border-radius: 999px; padding: 7px 10px; color: var(--muted); font-size: 12px; }
    .search-results { display: grid; gap: 8px; max-height: 220px; overflow: auto; }
    .result { border: 1px solid var(--line); border-radius: 12px; padding: 11px 12px; background: rgba(8, 17, 31, 0.65); cursor: pointer; transition: 140ms ease; }
    .result:hover, .result.active { border-color: rgba(88, 214, 255, 0.5); background: rgba(14, 29, 51, 0.95); }
    .result strong { display: block; margin-bottom: 4px; }
    .analysis-list, .edge-list, .neighbor-list { display: grid; gap: 8px; }
    .analysis-item, .edge-item, .neighbor-item { border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; background: rgba(6, 13, 24, 0.72); }
    .edge-item button, .neighbor-item button { width: auto; min-width: 0; padding: 0; background: none; border: 0; color: var(--accent); cursor: pointer; }
    .focus-tag { display: inline-flex; align-items: center; gap: 8px; padding: 7px 10px; border-radius: 999px; background: rgba(88, 214, 255, 0.12); color: var(--accent); border: 1px solid rgba(88, 214, 255, 0.22); font-size: 12px; }
    details { border: 1px solid var(--line); border-radius: 14px; padding: 10px 12px; background: rgba(6, 13, 24, 0.72); }
    summary { cursor: pointer; color: var(--text); }
    pre { margin: 12px 0 0; padding: 14px; background: rgba(3, 9, 18, 0.9); border-radius: 12px; overflow: auto; color: #b9d5ff; font-size: 12px; }
    .canvas-empty { position: absolute; inset: 0; display: none; align-items: center; justify-content: center; color: var(--muted); pointer-events: none; }
    .cluster-label { fill: rgba(236,243,255,0.82); font-size: 14px; font-weight: 600; pointer-events: none; }
    .cluster-ring { fill: rgba(255,255,255,0.02); stroke: rgba(131, 177, 237, 0.12); stroke-dasharray: 8 10; }
    .edge { stroke-linecap: round; }
    .node { cursor: pointer; }
    .node-label { font-size: 11px; fill: rgba(236,243,255,0.92); pointer-events: none; }
    .badge { padding: 3px 8px; border-radius: 999px; background: rgba(17, 30, 50, 0.85); color: var(--muted); font-size: 11px; border: 1px solid var(--line); }
    @media (max-width: 1380px) {
      .layout { grid-template-columns: 280px minmax(0, 1fr); }
      .details { grid-column: 1 / -1; border-left: 0; border-top: 1px solid var(--line); }
    }
    @media (max-width: 980px) {
      .layout { grid-template-columns: 1fr; }
      .sidebar, .details { border: 0; border-top: 1px solid var(--line); }
      .stage-card { min-height: 640px; }
    }
  </style>
</head>
<body>
  <header>
    <div class="hero">
      <div>
        <h1>{{.Title}}</h1>
        <div class="subtitle">{{.Summary}}</div>
      </div>
      <div class="hero-pills">
        <div class="pill">Nodes {{.NodeCount}}</div>
        <div class="pill">Edges {{.EdgeCount}}</div>
        {{if .Focus}}<div class="pill">Focus <code>{{.Focus}}</code></div>{{end}}
      </div>
    </div>
  </header>
  <main class="layout">
    <aside class="sidebar section">
      <div class="card">
        <h2>Controls</h2>
        <div class="controls-grid">
          <label>
            Search / locate
            <input id="searchInput" type="search" placeholder="Engine, README, graph...">
          </label>
          <label>
            Node type
            <select id="typeFilter"><option value="">All node types</option></select>
          </label>
          <label>
            Edge type
            <select id="edgeFilter"><option value="">All edge types</option></select>
          </label>
          <label>
            Cluster mode
            <select id="clusterMode">
              <option value="type">Cluster by type</option>
              <option value="module">Cluster by module</option>
              <option value="none">Free layout</option>
            </select>
          </label>
        </div>
        <div class="toggle-row" style="margin-top:12px;">
          <label class="toggle"><input id="documentMode" type="checkbox"> Document mode</label>
          <label class="toggle"><input id="hideSymbols" type="checkbox"> Hide symbol nodes</label>
          <label class="toggle"><input id="showLabels" type="checkbox" checked> Show labels</label>
          <label class="toggle"><input id="fadeUnrelated" type="checkbox" checked> Fade unrelated nodes on selection</label>
        </div>
        <div class="toolbar" style="margin-top:14px;">
          <button id="fitViewBtn" type="button">Zoom to fit</button>
          <button id="centerSelectedBtn" type="button">Center selected</button>
          <button id="resetSelectionBtn" type="button">Clear selection</button>
        </div>
      </div>
      <div class="card">
        <h2>Legend</h2>
        <div id="legend" class="legend"></div>
      </div>
      <div class="card">
        <h2>Search results</h2>
        <div id="resultCount" class="muted">Type to locate files, symbols, and documents.</div>
        <div id="searchResults" class="search-results"></div>
      </div>
      <div class="card">
        <h2>Cluster overview</h2>
        <div id="clusterSummary" class="analysis-list"></div>
      </div>
    </aside>

    <section class="content">
      <div class="stage-shell">
        <div class="stage-card">
          <div class="stage-topbar">
            <div>
              <h2 style="margin:0 0 6px;">Visual graph</h2>
              <div class="muted">Visual-first graph canvas with pan, zoom, drag, search, and document-aware focus.</div>
            </div>
            <div class="stage-metrics" id="metrics"></div>
          </div>
          <div class="stage-body">
            <svg id="graphSurface" viewBox="0 0 1400 900" aria-label="Interactive graph canvas">
              <defs>
                <filter id="glow"><feGaussianBlur stdDeviation="5" result="blur"></feGaussianBlur><feMerge><feMergeNode in="blur"></feMergeNode><feMergeNode in="SourceGraphic"></feMergeNode></feMerge></filter>
              </defs>
              <g id="viewport">
                <g id="clusterLayer"></g>
                <g id="edgeLayer"></g>
                <g id="nodeLayer"></g>
                <g id="labelLayer"></g>
              </g>
            </svg>
            <div id="canvasEmpty" class="canvas-empty">No nodes match the current filters.</div>
          </div>
          <div class="stage-hud">
            <div class="chip-row" id="focusChips"></div>
            <div class="chip-row">
              <div class="chip">Graph analysis</div>
              <div class="chip">Bridge files</div>
              <div class="chip">Reading paths</div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <aside class="details section">
      <div class="card">
        <h2>Selected node</h2>
        <div id="selectionState" class="muted">Pick a node to inspect its neighborhood, evidence, and related graph analysis.</div>
        <div id="selectionMeta" style="margin-top:12px;"></div>
      </div>
      <div class="card">
        <h2>Neighborhood</h2>
        <div id="neighbors" class="neighbor-list"></div>
      </div>
      <div class="card">
        <h2>Edge evidence</h2>
        <div id="edgeEvidence" class="edge-list"></div>
      </div>
      <div class="card">
        <h2>Graph analysis</h2>
        <div id="analysisPanel" class="analysis-list"></div>
      </div>
      <div class="card">
        <h2>Raw payload</h2>
        <details>
          <summary>Open graph JSON</summary>
          <pre id="rawPayload"></pre>
        </details>
      </div>
    </aside>
  </main>

  <script>
    const graph = {{.GraphJSON}};

    const svg = document.getElementById('graphSurface');
    const viewport = document.getElementById('viewport');
    const clusterLayer = document.getElementById('clusterLayer');
    const edgeLayer = document.getElementById('edgeLayer');
    const nodeLayer = document.getElementById('nodeLayer');
    const labelLayer = document.getElementById('labelLayer');
    const canvasEmpty = document.getElementById('canvasEmpty');
    const searchInput = document.getElementById('searchInput');
    const typeFilter = document.getElementById('typeFilter');
    const edgeFilter = document.getElementById('edgeFilter');
    const clusterMode = document.getElementById('clusterMode');
    const documentMode = document.getElementById('documentMode');
    const hideSymbols = document.getElementById('hideSymbols');
    const showLabels = document.getElementById('showLabels');
    const fadeUnrelated = document.getElementById('fadeUnrelated');
    const fitViewBtn = document.getElementById('fitViewBtn');
    const centerSelectedBtn = document.getElementById('centerSelectedBtn');
    const resetSelectionBtn = document.getElementById('resetSelectionBtn');
    const legend = document.getElementById('legend');
    const metrics = document.getElementById('metrics');
    const searchResults = document.getElementById('searchResults');
    const resultCount = document.getElementById('resultCount');
    const clusterSummary = document.getElementById('clusterSummary');
    const focusChips = document.getElementById('focusChips');
    const selectionState = document.getElementById('selectionState');
    const selectionMeta = document.getElementById('selectionMeta');
    const neighbors = document.getElementById('neighbors');
    const edgeEvidence = document.getElementById('edgeEvidence');
    const analysisPanel = document.getElementById('analysisPanel');
    const rawPayload = document.getElementById('rawPayload');

    rawPayload.textContent = JSON.stringify(graph, null, 2);

    const palette = {
      file: getCss('--file'),
      symbol: getCss('--symbol'),
      document: getCss('--document'),
      module: getCss('--module'),
      package: getCss('--package'),
      import: getCss('--import')
    };

    const state = {
      query: '',
      nodeType: '',
      edgeType: '',
      clusterMode: 'type',
      documentMode: false,
      hideSymbols: false,
      showLabels: true,
      fadeUnrelated: true,
      selectedId: graph.focus ? graph.nodes.find(function (node) {
        return node.id === graph.focus || node.file === graph.focus || node.name === graph.focus || node.label === graph.focus;
      })?.id || null : null,
      transform: { x: 0, y: 0, scale: 1 },
      draggingNode: null,
      panning: false,
      pointer: null
    };

    const nodes = graph.nodes.map(function (node, index) {
      return Object.assign({}, node, {
        index: index,
        color: palette[node.type] || '#7aa1db',
        radius: radiusFor(node.type),
        cluster: '',
        x: 0,
        y: 0,
        fx: null,
        fy: null
      });
    });
    const nodeById = new Map(nodes.map(function (node) { return [node.id, node]; }));
    const edges = graph.edges.filter(function (edge) {
      return nodeById.has(edge.source) && nodeById.has(edge.target);
    }).map(function (edge, index) {
      return Object.assign({}, edge, { index: index, key: edge.source + '|' + edge.target + '|' + edge.type + '|' + index });
    });
    const adjacency = new Map();
    nodes.forEach(function (node) { adjacency.set(node.id, new Set()); });
    edges.forEach(function (edge) {
      adjacency.get(edge.source).add(edge.target);
      adjacency.get(edge.target).add(edge.source);
    });

    populateFilters();
    renderLegend();
    renderAnalysisPanel();
    seedLayout();
    runLayout(130);
    bindEvents();
    render();

    function getCss(name) {
      return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    }

    function radiusFor(type) {
      if (type === 'module') return 13;
      if (type === 'package') return 11;
      if (type === 'document') return 10;
      if (type === 'file') return 9;
      if (type === 'import') return 7;
      return 6;
    }

    function populateFilters() {
      unique(nodes.map(function (node) { return node.type; })).forEach(function (type) {
        const option = document.createElement('option');
        option.value = type;
        option.textContent = type;
        typeFilter.appendChild(option);
      });
      unique(edges.map(function (edge) { return edge.type; })).forEach(function (type) {
        const option = document.createElement('option');
        option.value = type;
        option.textContent = type;
        edgeFilter.appendChild(option);
      });
    }

    function unique(values) {
      return Array.from(new Set(values.filter(Boolean))).sort();
    }

    function renderLegend() {
      const counts = countBy(nodes, function (node) { return node.type; });
      legend.innerHTML = '';
      Object.keys(counts).sort().forEach(function (type) {
        const item = document.createElement('div');
        item.className = 'legend-item';
        item.innerHTML = '<span class="swatch" style="color:' + (palette[type] || '#7aa1db') + '; background:' + (palette[type] || '#7aa1db') + '"></span><strong>' + escapeHTML(type) + '</strong><span class="muted">' + counts[type] + '</span>';
        legend.appendChild(item);
      });
    }

    function renderAnalysisPanel() {
      const analysis = graph.analysis || {};
      analysisPanel.innerHTML = '';
      appendAnalysisList('Top imports', analysis.top_imports || []);
      appendAnalysisList('Most connected files', analysis.most_connected_files || []);
      appendAnalysisList('Bridge files', analysis.bridge_files || []);
      appendAnalysisList('Hotspot files', analysis.hotspot_files || []);
      appendTextList('Relation highlights', analysis.relation_highlights || []);
      appendReadingPaths('Reading paths', analysis.reading_paths || []);
      appendTextList('Recommended files', analysis.recommended_files || []);
      if (!analysisPanel.children.length) {
        analysisPanel.innerHTML = '<div class="muted">No graph analysis available.</div>';
      }
    }

    function appendAnalysisList(title, items) {
      if (!items || !items.length) return;
      const block = document.createElement('div');
      block.className = 'analysis-item';
      block.innerHTML = '<strong>' + escapeHTML(title) + '</strong>';
      items.forEach(function (item) {
        const row = document.createElement('div');
        row.style.marginTop = '8px';
        row.innerHTML = '<span class="badge">' + escapeHTML(item.name || item.Name || '') + '</span> <span class="muted">' + escapeHTML(String(item.count ?? item.Count ?? '')) + '</span>';
        block.appendChild(row);
      });
      analysisPanel.appendChild(block);
    }

    function appendTextList(title, items) {
      if (!items || !items.length) return;
      const block = document.createElement('div');
      block.className = 'analysis-item';
      block.innerHTML = '<strong>' + escapeHTML(title) + '</strong>';
      items.forEach(function (item) {
        const row = document.createElement('div');
        row.className = 'muted';
        row.style.marginTop = '8px';
        row.textContent = item;
        block.appendChild(row);
      });
      analysisPanel.appendChild(block);
    }

    function appendReadingPaths(title, items) {
      if (!items || !items.length) return;
      const block = document.createElement('div');
      block.className = 'analysis-item';
      block.innerHTML = '<strong>' + escapeHTML(title) + '</strong>';
      items.forEach(function (item) {
        const row = document.createElement('div');
        row.style.marginTop = '8px';
        const path = Array.isArray(item.path || item.Path) ? (item.path || item.Path).join(' → ') : '';
        row.innerHTML = '<div><span class="badge">' + escapeHTML(item.entry || item.Entry || '') + '</span></div><div class="muted" style="margin-top:6px;">' + escapeHTML(path) + '</div>' + ((item.reason || item.Reason) ? '<div class="muted" style="margin-top:4px;">' + escapeHTML(item.reason || item.Reason) + '</div>' : '');
        block.appendChild(row);
      });
      analysisPanel.appendChild(block);
    }

    function bindEvents() {
      searchInput.addEventListener('input', function (event) {
        state.query = event.target.value.trim().toLowerCase();
        renderSearchResults();
        render();
      });
      typeFilter.addEventListener('change', function (event) {
        state.nodeType = event.target.value;
        render();
      });
      edgeFilter.addEventListener('change', function (event) {
        state.edgeType = event.target.value;
        render();
      });
      clusterMode.addEventListener('change', function (event) {
        state.clusterMode = event.target.value;
        seedLayout();
        runLayout(110);
        render();
      });
      documentMode.addEventListener('change', function (event) {
        state.documentMode = event.target.checked;
        render();
      });
      hideSymbols.addEventListener('change', function (event) {
        state.hideSymbols = event.target.checked;
        render();
      });
      showLabels.addEventListener('change', function (event) {
        state.showLabels = event.target.checked;
        render();
      });
      fadeUnrelated.addEventListener('change', function (event) {
        state.fadeUnrelated = event.target.checked;
        render();
      });
      fitViewBtn.addEventListener('click', fitView);
      centerSelectedBtn.addEventListener('click', function () {
        if (state.selectedId && nodeById.has(state.selectedId)) centerOnNode(nodeById.get(state.selectedId));
      });
      resetSelectionBtn.addEventListener('click', function () {
        state.selectedId = null;
        render();
      });
      svg.addEventListener('wheel', onWheel, { passive: false });
      svg.addEventListener('pointerdown', onPointerDown);
      svg.addEventListener('pointermove', onPointerMove);
      svg.addEventListener('pointerup', onPointerUp);
      svg.addEventListener('pointerleave', onPointerUp);
    }

    function onWheel(event) {
      event.preventDefault();
      const factor = event.deltaY < 0 ? 1.1 : 0.9;
      const point = svgPoint(event.clientX, event.clientY);
      state.transform.x = point.x - (point.x - state.transform.x) * factor;
      state.transform.y = point.y - (point.y - state.transform.y) * factor;
      state.transform.scale = clamp(state.transform.scale * factor, 0.35, 3.2);
      applyTransform();
    }

    function onPointerDown(event) {
      const target = event.target;
      if (target && target.dataset && target.dataset.nodeId) {
        const node = nodeById.get(target.dataset.nodeId);
        state.draggingNode = node;
        state.pointer = { x: event.clientX, y: event.clientY };
        svg.setPointerCapture(event.pointerId);
        return;
      }
      state.panning = true;
      state.pointer = { x: event.clientX, y: event.clientY };
      svg.setPointerCapture(event.pointerId);
    }

    function onPointerMove(event) {
      if (!state.pointer) return;
      const dx = event.clientX - state.pointer.x;
      const dy = event.clientY - state.pointer.y;
      state.pointer = { x: event.clientX, y: event.clientY };
      if (state.draggingNode) {
        state.draggingNode.x += dx / state.transform.scale;
        state.draggingNode.y += dy / state.transform.scale;
        state.draggingNode.fx = state.draggingNode.x;
        state.draggingNode.fy = state.draggingNode.y;
        render();
      } else if (state.panning) {
        state.transform.x += dx;
        state.transform.y += dy;
        applyTransform();
      }
    }

    function onPointerUp() {
      state.draggingNode = null;
      state.panning = false;
      state.pointer = null;
    }

    function seedLayout() {
      const width = 1400;
      const height = 900;
      const visibleNodes = nodes.slice();
      const groups = buildGroups(visibleNodes, state.clusterMode);
      const centers = Array.from(groups.keys()).reduce(function (acc, key, index, arr) {
        const angle = (Math.PI * 2 * index) / Math.max(arr.length, 1);
        const radius = arr.length === 1 ? 0 : Math.min(width, height) * 0.28;
        acc[key] = {
          x: width / 2 + Math.cos(angle) * radius,
          y: height / 2 + Math.sin(angle) * radius * 0.74
        };
        return acc;
      }, {});
      visibleNodes.forEach(function (node, index) {
        const cluster = groupKey(node, state.clusterMode);
        node.cluster = cluster;
        const center = centers[cluster] || { x: width / 2, y: height / 2 };
        const angle = (index * 0.73) % (Math.PI * 2);
        const radius = 30 + (index % 14) * 9;
        node.x = center.x + Math.cos(angle) * radius;
        node.y = center.y + Math.sin(angle) * radius;
        node.fx = null;
        node.fy = null;
      });
      state.transform = { x: 0, y: 0, scale: 1 };
      applyTransform();
      renderClusterSummary(groups);
      renderSearchResults();
    }

    function runLayout(iterations) {
      const activeNodes = nodes;
      const activeEdges = edges;
      for (let step = 0; step < iterations; step++) {
        activeNodes.forEach(function (node) {
          node.vx = (node.vx || 0) * 0.87;
          node.vy = (node.vy || 0) * 0.87;
        });

        for (let i = 0; i < activeNodes.length; i++) {
          const a = activeNodes[i];
          for (let j = i + 1; j < activeNodes.length; j++) {
            const b = activeNodes[j];
            let dx = a.x - b.x;
            let dy = a.y - b.y;
            let dist2 = dx * dx + dy * dy + 0.1;
            let force = 560 / dist2;
            if (a.cluster === b.cluster) force *= 1.25;
            const inv = 1 / Math.sqrt(dist2);
            dx *= inv;
            dy *= inv;
            a.vx += dx * force;
            a.vy += dy * force;
            b.vx -= dx * force;
            b.vy -= dy * force;
          }
        }

        activeEdges.forEach(function (edge) {
          const source = nodeById.get(edge.source);
          const target = nodeById.get(edge.target);
          if (!source || !target) return;
          let dx = target.x - source.x;
          let dy = target.y - source.y;
          let dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const desired = source.cluster === target.cluster ? 72 : 126;
          const force = (dist - desired) * 0.0048;
          dx /= dist;
          dy /= dist;
          source.vx += dx * force * 6;
          source.vy += dy * force * 6;
          target.vx -= dx * force * 6;
          target.vy -= dy * force * 6;
        });

        const centers = buildCenters(activeNodes, state.clusterMode);
        activeNodes.forEach(function (node) {
          const center = centers[node.cluster] || { x: 700, y: 450 };
          node.vx += (center.x - node.x) * (state.clusterMode === 'none' ? 0.0006 : 0.0036);
          node.vy += (center.y - node.y) * (state.clusterMode === 'none' ? 0.0006 : 0.0036);
          if (node.fx != null) node.x = node.fx; else node.x += node.vx;
          if (node.fy != null) node.y = node.fy; else node.y += node.vy;
          node.x = clamp(node.x, 80, 1320);
          node.y = clamp(node.y, 70, 830);
        });
      }
      fitView();
    }

    function buildGroups(list, mode) {
      const map = new Map();
      list.forEach(function (node) {
        const key = groupKey(node, mode);
        if (!map.has(key)) map.set(key, []);
        map.get(key).push(node);
      });
      return map;
    }

    function buildCenters(list, mode) {
      const groups = buildGroups(list, mode);
      const keys = Array.from(groups.keys());
      const centers = {};
      keys.forEach(function (key, index) {
        const angle = (Math.PI * 2 * index) / Math.max(keys.length, 1);
        const radius = mode === 'none' ? 0 : 250;
        centers[key] = {
          x: 700 + Math.cos(angle) * radius,
          y: 450 + Math.sin(angle) * radius * 0.72
        };
      });
      return centers;
    }

    function groupKey(node, mode) {
      if (mode === 'module') {
        if (node.type === 'module') return node.label || node.id;
        const file = node.file || '';
        if (!file) return node.type;
        const parts = file.split('/');
        if (parts.length <= 1) return 'root';
        return parts.slice(0, Math.min(2, parts.length - 1)).join('/');
      }
      if (mode === 'none') return 'graph';
      return node.type || 'unknown';
    }

    function visibleNodeMap() {
      const visible = new Map();
      const query = state.query;
      let seedIds = null;
      if (state.documentMode) {
        seedIds = new Set();
        edges.forEach(function (edge) {
          const source = nodeById.get(edge.source);
          const target = nodeById.get(edge.target);
          const docEdge = (source && source.type === 'document') || (target && target.type === 'document');
          if (docEdge) {
            seedIds.add(edge.source);
            seedIds.add(edge.target);
          }
        });
      }
      nodes.forEach(function (node) {
        if (state.hideSymbols && node.type === 'symbol') return;
        if (state.nodeType && node.type !== state.nodeType) return;
        if (seedIds && !seedIds.has(node.id)) return;
        if (query) {
          const hay = [node.label, node.name, node.file, node.kind, node.id].filter(Boolean).join(' ').toLowerCase();
          if (!hay.includes(query) && node.id !== state.selectedId) return;
        }
        visible.set(node.id, node);
      });
      return visible;
    }

    function visibleEdges(visible) {
      return edges.filter(function (edge) {
        if (state.edgeType && edge.type !== state.edgeType) return false;
        return visible.has(edge.source) && visible.has(edge.target);
      });
    }

    function render() {
      const visible = visibleNodeMap();
      const activeEdges = visibleEdges(visible);
      const activeNodes = Array.from(visible.values());
      canvasEmpty.style.display = activeNodes.length ? 'none' : 'flex';
      renderMetrics(activeNodes, activeEdges);
      renderFocusChips(activeNodes, activeEdges);
      renderClusterBackdrop(activeNodes);
      renderEdges(activeEdges);
      renderNodes(activeNodes, activeEdges);
      renderLabelsLayer(activeNodes);
      renderSelection(activeEdges, activeNodes);
      renderSearchResults();
    }

    function renderMetrics(activeNodes, activeEdges) {
      const docs = activeNodes.filter(function (node) { return node.type === 'document'; }).length;
      const modules = activeNodes.filter(function (node) { return node.type === 'module'; }).length;
      metrics.innerHTML = [
        '<div class="metric">Visible nodes ' + activeNodes.length + '</div>',
        '<div class="metric">Visible edges ' + activeEdges.length + '</div>',
        '<div class="metric">Documents ' + docs + '</div>',
        '<div class="metric">Modules ' + modules + '</div>'
      ].join('');
    }

    function renderFocusChips(activeNodes, activeEdges) {
      const chips = [];
      if (state.selectedId && nodeById.has(state.selectedId)) {
        chips.push('<div class="focus-tag">Selected ' + escapeHTML(nodeById.get(state.selectedId).label || state.selectedId) + '</div>');
      }
      if (state.documentMode) chips.push('<div class="focus-tag">Document mode</div>');
      if (state.nodeType) chips.push('<div class="focus-tag">Node filter ' + escapeHTML(state.nodeType) + '</div>');
      if (state.edgeType) chips.push('<div class="focus-tag">Edge filter ' + escapeHTML(state.edgeType) + '</div>');
      if (!chips.length) chips.push('<div class="chip">Showing ' + activeNodes.length + ' nodes and ' + activeEdges.length + ' edges</div>');
      focusChips.innerHTML = chips.join('');
    }

    function renderClusterBackdrop(activeNodes) {
      clusterLayer.innerHTML = '';
      const groups = buildGroups(activeNodes, state.clusterMode);
      Array.from(groups.entries()).forEach(function (entry) {
        const key = entry[0];
        const list = entry[1];
        if (!list.length || state.clusterMode === 'none') return;
        const bounds = list.reduce(function (acc, node) {
          acc.minX = Math.min(acc.minX, node.x);
          acc.maxX = Math.max(acc.maxX, node.x);
          acc.minY = Math.min(acc.minY, node.y);
          acc.maxY = Math.max(acc.maxY, node.y);
          return acc;
        }, { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
        const width = Math.max(160, bounds.maxX - bounds.minX + 110);
        const height = Math.max(140, bounds.maxY - bounds.minY + 100);
        const cx = (bounds.minX + bounds.maxX) / 2;
        const cy = (bounds.minY + bounds.maxY) / 2;
        const ellipse = document.createElementNS('http://www.w3.org/2000/svg', 'ellipse');
        ellipse.setAttribute('class', 'cluster-ring');
        ellipse.setAttribute('cx', cx);
        ellipse.setAttribute('cy', cy);
        ellipse.setAttribute('rx', width / 2);
        ellipse.setAttribute('ry', height / 2);
        clusterLayer.appendChild(ellipse);
        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        text.setAttribute('class', 'cluster-label');
        text.setAttribute('x', cx - width / 2 + 16);
        text.setAttribute('y', cy - height / 2 + 20);
        text.textContent = key;
        clusterLayer.appendChild(text);
      });
    }

    function renderEdges(activeEdges) {
      edgeLayer.innerHTML = '';
      const selectedNeighbors = selectedNeighborhood();
      activeEdges.forEach(function (edge) {
        const source = nodeById.get(edge.source);
        const target = nodeById.get(edge.target);
        const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
        line.setAttribute('x1', source.x);
        line.setAttribute('y1', source.y);
        line.setAttribute('x2', target.x);
        line.setAttribute('y2', target.y);
        line.setAttribute('class', 'edge');
        line.setAttribute('stroke', edgeColor(edge.type));
        line.setAttribute('stroke-width', edgeStroke(edge));
        line.setAttribute('opacity', edgeOpacity(edge, selectedNeighbors));
        edgeLayer.appendChild(line);
      });
    }

    function renderNodes(activeNodes, activeEdges) {
      nodeLayer.innerHTML = '';
      const selectedNeighbors = selectedNeighborhood();
      activeNodes.forEach(function (node) {
        const group = document.createElementNS('http://www.w3.org/2000/svg', 'g');
        group.setAttribute('class', 'node');
        group.dataset.nodeId = node.id;
        group.setAttribute('transform', 'translate(' + node.x + ' ' + node.y + ')');
        group.setAttribute('opacity', nodeOpacity(node, selectedNeighbors));
        const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
        circle.dataset.nodeId = node.id;
        circle.setAttribute('r', node.radius + (state.selectedId === node.id ? 4 : 0));
        circle.setAttribute('fill', node.color);
        circle.setAttribute('stroke', state.selectedId === node.id ? '#ffffff' : 'rgba(255,255,255,0.16)');
        circle.setAttribute('stroke-width', state.selectedId === node.id ? '2.6' : '1.2');
        circle.setAttribute('filter', state.selectedId === node.id ? 'url(#glow)' : '');
        circle.addEventListener('click', function (event) {
          event.stopPropagation();
          state.selectedId = node.id;
          render();
        });
        group.appendChild(circle);
        nodeLayer.appendChild(group);
      });
    }

    function renderLabelsLayer(activeNodes) {
      labelLayer.innerHTML = '';
      if (!state.showLabels) return;
      const selectedNeighbors = selectedNeighborhood();
      activeNodes.forEach(function (node) {
        if (activeNodes.length > 180 && state.selectedId !== node.id && !selectedNeighbors.has(node.id) && node.type === 'symbol') return;
        const text = document.createElementNS('http://www.w3.org/2000/svg', 'text');
        text.setAttribute('class', 'node-label');
        text.setAttribute('x', node.x + node.radius + 6);
        text.setAttribute('y', node.y + 4);
        text.setAttribute('opacity', nodeOpacity(node, selectedNeighbors));
        text.textContent = node.label || node.name || node.id;
        labelLayer.appendChild(text);
      });
    }

    function renderSelection(activeEdges, activeNodes) {
      if (!state.selectedId || !nodeById.has(state.selectedId)) {
        selectionState.innerHTML = 'Pick a node to inspect its neighborhood, evidence, and related graph analysis.';
        selectionMeta.innerHTML = '';
        neighbors.innerHTML = '<div class="muted">No node selected.</div>';
        edgeEvidence.innerHTML = '<div class="muted">Select a node to inspect supporting edges.</div>';
        return;
      }
      const node = nodeById.get(state.selectedId);
      const incident = activeEdges.filter(function (edge) {
        return edge.source === node.id || edge.target === node.id;
      });
      const neighborIds = Array.from(adjacency.get(node.id) || []).filter(function (id) { return activeNodes.some(function (n) { return n.id === id; }); });
      selectionState.innerHTML = '<strong>' + escapeHTML(node.label || node.id) + '</strong><div class="muted" style="margin-top:6px;">' + escapeHTML(node.type) + '</div>';
      selectionMeta.innerHTML = [
        node.file ? '<span class="badge">file ' + escapeHTML(node.file) + '</span>' : '',
        node.name ? '<span class="badge">name ' + escapeHTML(node.name) + '</span>' : '',
        node.kind ? '<span class="badge">kind ' + escapeHTML(node.kind) + '</span>' : '',
        node.language ? '<span class="badge">language ' + escapeHTML(node.language) + '</span>' : '',
        node.line ? '<span class="badge">line ' + escapeHTML(String(node.line)) + '</span>' : ''
      ].join(' ');

      neighbors.innerHTML = '';
      if (!neighborIds.length) {
        neighbors.innerHTML = '<div class="muted">No visible neighbors.</div>';
      } else {
        neighborIds.slice(0, 18).forEach(function (id) {
          const neighbor = nodeById.get(id);
          const item = document.createElement('div');
          item.className = 'neighbor-item';
          item.innerHTML = '<button type="button">' + escapeHTML(neighbor.label || neighbor.id) + '</button><div class="muted" style="margin-top:6px;">' + escapeHTML(neighbor.type + (neighbor.file ? ' · ' + neighbor.file : '')) + '</div>';
          item.querySelector('button').addEventListener('click', function () {
            state.selectedId = neighbor.id;
            centerOnNode(neighbor);
            render();
          });
          neighbors.appendChild(item);
        });
      }

      edgeEvidence.innerHTML = '';
      if (!incident.length) {
        edgeEvidence.innerHTML = '<div class="muted">No visible incident edges.</div>';
      } else {
        incident.slice(0, 24).forEach(function (edge) {
          const item = document.createElement('div');
          item.className = 'edge-item';
          const otherId = edge.source === node.id ? edge.target : edge.source;
          const other = nodeById.get(otherId);
          item.innerHTML = '<div><span class="badge">' + escapeHTML(edge.type) + '</span></div><div style="margin-top:8px;"><button type="button">' + escapeHTML(other ? (other.label || other.id) : otherId) + '</button></div><div class="muted" style="margin-top:6px;">' + escapeHTML(edge.evidence || 'no evidence') + '</div>';
          item.querySelector('button').addEventListener('click', function () {
            if (other) {
              state.selectedId = other.id;
              centerOnNode(other);
              render();
            }
          });
          edgeEvidence.appendChild(item);
        });
      }
    }

    function renderSearchResults() {
      const matches = searchMatches();
      resultCount.textContent = matches.length ? matches.length + ' matching nodes' : 'Type to locate files, symbols, and documents.';
      searchResults.innerHTML = '';
      matches.slice(0, 24).forEach(function (node) {
        const item = document.createElement('div');
        item.className = 'result' + (state.selectedId === node.id ? ' active' : '');
        item.innerHTML = '<strong>' + escapeHTML(node.label || node.id) + '</strong><div class="muted">' + escapeHTML(node.type + (node.file ? ' · ' + node.file : '')) + '</div>';
        item.addEventListener('click', function () {
          state.selectedId = node.id;
          centerOnNode(node);
          render();
        });
        searchResults.appendChild(item);
      });
    }

    function renderClusterSummary(groups) {
      clusterSummary.innerHTML = '';
      Array.from(groups.entries()).sort(function (a, b) { return b[1].length - a[1].length; }).slice(0, 12).forEach(function (entry) {
        const item = document.createElement('div');
        item.className = 'analysis-item';
        item.innerHTML = '<strong>' + escapeHTML(entry[0]) + '</strong><div class="muted" style="margin-top:8px;">' + entry[1].length + ' nodes</div>';
        clusterSummary.appendChild(item);
      });
      if (!clusterSummary.children.length) {
        clusterSummary.innerHTML = '<div class="muted">No clusters available.</div>';
      }
    }

    function searchMatches() {
      if (!state.query) return nodes.slice(0, 18);
      return nodes.filter(function (node) {
        const hay = [node.label, node.name, node.file, node.kind, node.id].filter(Boolean).join(' ').toLowerCase();
        return hay.includes(state.query);
      }).sort(function (a, b) {
        return scoreMatch(b, state.query) - scoreMatch(a, state.query);
      });
    }

    function scoreMatch(node, query) {
      const text = [node.label, node.name, node.file, node.id].filter(Boolean).join(' ').toLowerCase();
      if (text === query) return 100;
      if ((node.label || '').toLowerCase() === query) return 96;
      if (text.startsWith(query)) return 84;
      if (text.includes(query)) return 68;
      return 0;
    }

    function edgeColor(type) {
      if (String(type).indexOf('mentions_') === 0) return 'rgba(143,247,193,0.58)';
      if (type === 'imports') return 'rgba(255,150,113,0.42)';
      if (type === 'defines') return 'rgba(255,209,102,0.46)';
      if (type === 'belongs_to' || type === 'declares_package' || type === 'describes') return 'rgba(179,156,255,0.38)';
      return 'rgba(122,161,219,0.28)';
    }

    function edgeStroke(edge) {
      if (state.selectedId && (edge.source === state.selectedId || edge.target === state.selectedId)) return 2.6;
      if (String(edge.type).indexOf('mentions_') === 0) return 2.1;
      return 1.2;
    }

    function selectedNeighborhood() {
      const set = new Set();
      if (!state.selectedId || !adjacency.has(state.selectedId)) return set;
      set.add(state.selectedId);
      adjacency.get(state.selectedId).forEach(function (id) { set.add(id); });
      return set;
    }

    function edgeOpacity(edge, selectedNeighbors) {
      if (!state.selectedId || !state.fadeUnrelated) return 0.78;
      if (edge.source === state.selectedId || edge.target === state.selectedId) return 0.98;
      if (selectedNeighbors.has(edge.source) && selectedNeighbors.has(edge.target)) return 0.42;
      return 0.08;
    }

    function nodeOpacity(node, selectedNeighbors) {
      if (!state.selectedId || !state.fadeUnrelated) return 0.96;
      if (selectedNeighbors.has(node.id)) return 1;
      return 0.12;
    }

    function fitView() {
      const visible = Array.from(visibleNodeMap().values());
      if (!visible.length) return;
      const bounds = visible.reduce(function (acc, node) {
        acc.minX = Math.min(acc.minX, node.x - node.radius);
        acc.maxX = Math.max(acc.maxX, node.x + node.radius);
        acc.minY = Math.min(acc.minY, node.y - node.radius);
        acc.maxY = Math.max(acc.maxY, node.y + node.radius);
        return acc;
      }, { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
      const width = Math.max(bounds.maxX - bounds.minX, 240);
      const height = Math.max(bounds.maxY - bounds.minY, 220);
      const scale = Math.min(1200 / width, 760 / height, 1.65);
      state.transform.scale = clamp(scale, 0.35, 3.2);
      state.transform.x = 700 - ((bounds.minX + bounds.maxX) / 2) * state.transform.scale;
      state.transform.y = 450 - ((bounds.minY + bounds.maxY) / 2) * state.transform.scale;
      applyTransform();
    }

    function centerOnNode(node) {
      state.transform.x = 700 - node.x * state.transform.scale;
      state.transform.y = 450 - node.y * state.transform.scale;
      applyTransform();
    }

    function applyTransform() {
      viewport.setAttribute('transform', 'translate(' + state.transform.x + ' ' + state.transform.y + ') scale(' + state.transform.scale + ')');
    }

    function svgPoint(clientX, clientY) {
      const rect = svg.getBoundingClientRect();
      return {
        x: clientX - rect.left,
        y: clientY - rect.top
      };
    }

    function clamp(value, min, max) {
      return Math.max(min, Math.min(max, value));
    }

    function countBy(list, pick) {
      return list.reduce(function (acc, item) {
        const key = pick(item);
        acc[key] = (acc[key] || 0) + 1;
        return acc;
      }, {});
    }

    function escapeHTML(value) {
      return String(value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
    }
  </script>
</body>
</html>`))
