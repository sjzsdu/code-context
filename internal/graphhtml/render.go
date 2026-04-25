package graphhtml

import (
	"encoding/json"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sjzsdu/code-context/internal/api"
)

type view struct {
	Title            string
	Focus            string
	Summary          string
	NodeCount        int
	EdgeCount        int
	GraphJSON        template.JS
	NodeContentsJSON template.JS
}

type nodeContent struct {
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Content   string `json:"content,omitempty"`
	Language  string `json:"language,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Path      string `json:"path,omitempty"`
}

func Render(w io.Writer, root string, graph *api.GraphExport) error {
	payload, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	contentsPayload, err := json.Marshal(buildNodeContents(root, graph))
	if err != nil {
		return err
	}
	return pageTemplate.Execute(w, view{
		Title:            "code-context graph view",
		Focus:            graph.Focus,
		Summary:          graph.Summary,
		NodeCount:        len(graph.Nodes),
		EdgeCount:        len(graph.Edges),
		GraphJSON:        template.JS(payload),
		NodeContentsJSON: template.JS(contentsPayload),
	})
}

func buildNodeContents(root string, graph *api.GraphExport) map[string]nodeContent {
	contents := make(map[string]nodeContent, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.FilePath == "" {
			continue
		}
		fullPath := filepath.Join(root, node.FilePath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		text := normalizeNewlines(string(data))
		content, truncated := excerptForNode(node, text)
		contents[node.ID] = nodeContent{
			Title:     node.Label,
			Subtitle:  node.FilePath,
			Content:   content,
			Language:  string(node.Language),
			Truncated: truncated,
			Kind:      node.Type,
			Path:      node.FilePath,
		}
	}
	return contents
}

func excerptForNode(node api.GraphNode, text string) (string, bool) {
	const maxChars = 6000
	lines := strings.Split(text, "\n")
	if node.Type == "symbol" && node.Line > 0 {
		lineIndex := minInt(maxInt(0, node.Line-1), len(lines))
		start := minInt(maxInt(0, lineIndex-7), len(lines))
		end := minInt(len(lines), lineIndex+20)
		if start > end {
			start = end
		}
		chunk := strings.Join(lines[start:end], "\n")
		return trimContent(chunk, maxChars)
	}
	if node.Type == "document" {
		return trimContent(text, maxChars)
	}
	return trimContent(text, maxChars)
}

func trimContent(text string, maxChars int) (string, bool) {
	if len(text) <= maxChars {
		return text, false
	}
	return text[:maxChars] + "\n\n... [truncated]", true
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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
      --bg: #06101d;
      --bg-soft: rgba(9, 18, 32, 0.9);
      --bg-card: rgba(12, 24, 43, 0.84);
      --panel: rgba(8, 16, 29, 0.92);
      --line: rgba(120, 158, 214, 0.18);
      --line-strong: rgba(120, 158, 214, 0.32);
      --text: #eaf2ff;
      --muted: #95a7c6;
      --accent: #59d8ff;
      --accent-soft: rgba(89, 216, 255, 0.16);
      --file: #62b3ff;
      --symbol: #ffd166;
      --document: #86f3b6;
      --module: #ff98d0;
      --package: #b79cff;
      --import: #ff9d76;
      --shadow: 0 28px 80px rgba(0, 0, 0, 0.42);
    }
    * { box-sizing: border-box; }
    html, body {
      margin: 0;
      min-height: 100%;
      background:
        radial-gradient(circle at top left, rgba(89, 216, 255, 0.16), transparent 25%),
        radial-gradient(circle at top right, rgba(183, 156, 255, 0.14), transparent 26%),
        var(--bg);
      color: var(--text);
      font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    body { display: flex; flex-direction: column; }
    header {
      position: sticky; top: 0; z-index: 10;
      border-bottom: 1px solid var(--line);
      background: rgba(6, 16, 29, 0.92);
      backdrop-filter: blur(16px);
      padding: 18px 22px 16px;
    }
    .hero { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; flex-wrap: wrap; }
    h1 { margin: 0 0 8px; font-size: 28px; letter-spacing: -0.03em; }
    .subtitle { color: var(--muted); max-width: 900px; line-height: 1.45; }
    .pill-row { display: flex; gap: 8px; flex-wrap: wrap; }
    .pill, .badge {
      display: inline-flex; align-items: center; gap: 8px;
      border-radius: 999px; border: 1px solid var(--line-strong);
      padding: 7px 11px; font-size: 12px; white-space: nowrap;
      background: rgba(15, 27, 47, 0.9);
    }
    main { display: grid; grid-template-columns: 320px minmax(0, 1fr) 360px; min-height: calc(100vh - 92px); }
    aside, .details { background: var(--panel); overflow: auto; }
    aside { border-right: 1px solid var(--line); }
    .details { border-left: 1px solid var(--line); }
    .center { min-width: 0; }
    .section { padding: 18px; }
    .card {
      background: var(--bg-card);
      border: 1px solid var(--line);
      border-radius: 18px;
      box-shadow: var(--shadow);
      padding: 16px;
      margin-bottom: 16px;
    }
    .card h2, .card h3 { margin: 0 0 12px; letter-spacing: -0.02em; }
    .muted { color: var(--muted); }
    .controls-grid { display: grid; gap: 12px; grid-template-columns: repeat(2, minmax(0, 1fr)); }
    label { display: grid; gap: 6px; font-size: 12px; color: var(--muted); }
    input[type="search"], select, button {
      width: 100%;
      border: 1px solid rgba(122, 161, 219, 0.26);
      background: rgba(5, 11, 20, 0.92);
      color: var(--text);
      border-radius: 12px;
      padding: 11px 12px;
      font: inherit;
    }
    button { cursor: pointer; transition: 140ms ease; }
    button:hover { border-color: rgba(89, 216, 255, 0.55); transform: translateY(-1px); }
    .toolbar { display: flex; gap: 10px; flex-wrap: wrap; }
    .toolbar button { width: auto; min-width: 120px; }
    .toggle-row { display: grid; gap: 10px; margin-top: 12px; }
    .toggle { display: flex; align-items: center; gap: 10px; color: var(--text); font-size: 13px; }
    .toggle input { width: 16px; height: 16px; }
    .legend { display: grid; gap: 10px; grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .legend-item, .result, .analysis-item, .detail-item {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: rgba(7, 14, 25, 0.78);
      padding: 10px 12px;
    }
    .selection-actions { display:grid; gap:8px; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 14px; }
    .selection-actions button { width:100%; }
    .swatch { width: 12px; height: 12px; border-radius: 50%; display: inline-block; margin-right: 8px; box-shadow: 0 0 14px currentColor; }
    .search-results, .analysis-list, .detail-list { display: grid; gap: 8px; }
    .result { cursor: pointer; transition: 120ms ease; }
    .result:hover, .result.active { border-color: rgba(89, 216, 255, 0.46); background: rgba(14, 28, 49, 0.96); }
    .stage-shell { padding: 18px; }
    .stage-card {
      background: linear-gradient(180deg, rgba(11, 22, 39, 0.92), rgba(5, 11, 20, 0.96));
      border: 1px solid var(--line);
      border-radius: 24px;
      overflow: hidden;
      box-shadow: var(--shadow);
      display: grid;
      grid-template-rows: auto 1fr auto;
      min-height: 740px;
    }
    .stage-topbar, .stage-footer {
      display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap;
      padding: 16px 18px;
      border-bottom: 1px solid var(--line);
      background: rgba(8, 16, 29, 0.78);
    }
    .stage-footer { border-bottom: 0; border-top: 1px solid var(--line); }
    .metrics, .chip-row { display: flex; gap: 8px; flex-wrap: wrap; }
    .metric { padding: 8px 10px; border-radius: 999px; background: rgba(11, 21, 37, 0.92); border: 1px solid var(--line); color: var(--muted); font-size: 12px; }
    .canvas-wrap { position: relative; min-height: 600px; }
    #graphSurface {
      width: 100%; height: 100%; min-height: 600px; display: block;
      background:
        radial-gradient(circle at 20% 20%, rgba(89,216,255,0.08), transparent 22%),
        radial-gradient(circle at 82% 18%, rgba(255,152,208,0.08), transparent 18%),
        radial-gradient(circle at 60% 80%, rgba(183,156,255,0.08), transparent 24%),
        linear-gradient(180deg, rgba(7, 17, 31, 0.98), rgba(4, 9, 18, 0.98));
      cursor: grab;
    }
    #graphSurface.dragging { cursor: grabbing; }
    .canvas-empty {
      position: absolute; inset: 0; display: none; align-items: center; justify-content: center;
      color: var(--muted); pointer-events: none;
    }
    .hover-card {
      position: absolute; min-width: 220px; max-width: 320px; pointer-events: none;
      background: rgba(7, 14, 25, 0.96); border: 1px solid var(--line-strong); border-radius: 14px;
      box-shadow: var(--shadow); padding: 12px 14px; display: none; z-index: 4;
    }
    .hover-card.visible { display: block; }
    .hover-card strong { display: block; margin-bottom: 6px; }
    .canvas-hint {
      position: absolute; left: 16px; bottom: 16px; z-index: 3;
      border: 1px solid var(--line); background: rgba(7, 14, 25, 0.82); border-radius: 999px;
      padding: 8px 10px; color: var(--muted); font-size: 11px;
    }
    .minimap-wrap {
      position: absolute; right: 16px; bottom: 16px; width: 180px; height: 128px;
      border: 1px solid var(--line-strong); background: rgba(7, 14, 25, 0.84); border-radius: 16px;
      overflow: hidden; box-shadow: var(--shadow); z-index: 3;
    }
    #minimapCanvas { width: 100%; height: 100%; display: block; }
    .minimap-wrap button { all: unset; cursor: pointer; display: block; width: 100%; height: 100%; }
    .context-menu {
      position: absolute; min-width: 180px; display: none; z-index: 6;
      border: 1px solid var(--line-strong); background: rgba(7, 14, 25, 0.98); border-radius: 14px; box-shadow: var(--shadow);
      overflow: hidden;
    }
    .context-menu.visible { display: block; }
    .context-menu button {
      all: unset; display: block; width: 100%; padding: 12px 14px; cursor: pointer; color: var(--text);
      border-bottom: 1px solid rgba(120, 158, 214, 0.12);
    }
    .context-menu button:last-child { border-bottom: 0; }
    .context-menu button:hover { background: rgba(89, 216, 255, 0.12); }
    .content-modal {
      position: fixed; inset: 0; display: none; align-items: center; justify-content: center; z-index: 20;
      background: rgba(2, 6, 12, 0.72); backdrop-filter: blur(10px);
    }
    .content-modal.visible { display: flex; }
    .content-modal-card {
      width: min(1100px, calc(100vw - 48px)); max-height: calc(100vh - 48px); overflow: hidden;
      border: 1px solid var(--line-strong); background: rgba(7, 14, 25, 0.98); border-radius: 22px; box-shadow: var(--shadow);
      display: grid; grid-template-rows: auto 1fr;
    }
    .content-modal-head { display:flex; justify-content:space-between; gap:12px; align-items:flex-start; padding:16px 18px; border-bottom:1px solid var(--line); }
    .content-modal-body { padding: 0 18px 18px; overflow: auto; }
    .content-modal-body pre { margin-top: 0; max-height: calc(100vh - 180px); }
    .content-modal-actions { display:flex; gap:10px; flex-wrap:wrap; margin-top:12px; }
    .close-button { width:auto; min-width:0; padding:8px 12px; }
    .action-button { width:auto; min-width:0; padding:8px 12px; }
    .content-frame {
      border: 1px solid var(--line); background: rgba(4, 9, 18, 0.95); border-radius: 16px; overflow: hidden;
    }
    .content-toolbar {
      display:flex; justify-content:space-between; gap:12px; align-items:center; padding:10px 12px; border-bottom:1px solid rgba(120, 158, 214, 0.12);
      background: rgba(10, 20, 37, 0.92);
    }
    .language-pill { border-color: rgba(89,216,255,0.28); color: var(--accent); }
    .content-view { margin: 0; padding: 16px; overflow: auto; max-height: calc(100vh - 250px); }
    .content-view.expanded { max-height: none; }
    .code-view { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.55; color: #dfe8ff; white-space: pre; }
    .text-view { font-family: Georgia, "Times New Roman", serif; font-size: 14px; line-height: 1.7; color: #e8f0ff; white-space: pre-wrap; }
    .tok-keyword { color: #ff7ab6; }
    .tok-string { color: #9cf59c; }
    .tok-comment { color: #7689a8; }
    .tok-number { color: #ffcf72; }
    .tok-type { color: #7fd5ff; }
    .tok-func { color: #c6a7ff; }
    .tok-path { color: #86f3b6; }
    .copy-feedback { color: var(--accent); font-size: 12px; }
    .focus-tag { background: var(--accent-soft); color: var(--accent); border-color: rgba(89,216,255,0.28); }
    summary { cursor: pointer; }
    pre {
      margin: 12px 0 0; padding: 14px; border-radius: 12px; overflow: auto;
      background: rgba(4, 9, 18, 0.95); color: #b7d2ff; font-size: 12px;
    }
    .detail-title { display: flex; justify-content: space-between; gap: 10px; align-items: center; }
    .detail-grid { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 12px; }
    .detail-button {
      width: auto; min-width: 0; padding: 0; border: 0; background: none; color: var(--accent); cursor: pointer;
    }
    @media (max-width: 1380px) {
      main { grid-template-columns: 300px minmax(0, 1fr); }
      .details { grid-column: 1 / -1; border-left: 0; border-top: 1px solid var(--line); }
    }
    @media (max-width: 980px) {
      main { grid-template-columns: 1fr; }
      aside, .details { border: 0; border-top: 1px solid var(--line); }
      .stage-card { min-height: 660px; }
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
      <div class="pill-row">
        <div class="pill">Nodes {{.NodeCount}}</div>
        <div class="pill">Edges {{.EdgeCount}}</div>
        {{if .Focus}}<div class="pill">Focus <code>{{.Focus}}</code></div>{{end}}
      </div>
    </div>
  </header>
  <main>
    <aside class="section">
      <div class="card">
        <h2>Controls</h2>
        <div class="controls-grid">
          <label>Search / locate<input id="searchInput" type="search" placeholder="Engine, README, graph..."></label>
          <label>Node type<select id="typeFilter"><option value="">All node types</option></select></label>
          <label>Edge type<select id="edgeFilter"><option value="">All edge types</option></select></label>
          <label>Cluster mode<select id="clusterMode"><option value="type">Cluster by type</option><option value="module">Cluster by module</option><option value="none">Free layout</option></select></label>
          <label>Focus depth<select id="focusDepth"><option value="all">Entire graph</option><option value="1">1-hop from selection</option><option value="2">2-hop from selection</option></select></label>
        </div>
        <div class="toggle-row">
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
        <h2>Graph analysis</h2>
        <div id="analysisPanel" class="analysis-list"></div>
      </div>
    </aside>

    <section class="center">
      <div class="stage-shell">
        <div class="stage-card">
          <div class="stage-topbar">
            <div>
              <h2 style="margin:0 0 6px;">Visual graph</h2>
              <div class="muted">Canvas-first graph view with pan, zoom, drag, hit-testing, and node / edge inspection.</div>
            </div>
            <div id="metrics" class="metrics"></div>
          </div>
          <div class="canvas-wrap">
            <canvas id="graphSurface" aria-label="Interactive graph canvas"></canvas>
            <div id="canvasEmpty" class="canvas-empty">No nodes match the current filters.</div>
            <div id="hoverCard" class="hover-card"></div>
            <div class="canvas-hint">Shift + drag to box zoom · double click node to pin</div>
            <div class="minimap-wrap"><button id="minimapButton" type="button" aria-label="Navigate graph minimap"><canvas id="minimapCanvas" aria-label="Graph minimap"></canvas></button></div>
          </div>
          <div class="stage-footer">
            <div id="focusChips" class="chip-row"></div>
            <div class="chip-row">
              <div class="badge">Graph analysis</div>
              <div class="badge">Bridge files</div>
              <div class="badge">Reading paths</div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <aside class="details section">
      <div class="card">
        <h2>Selection</h2>
        <div id="selectionSummary" class="muted">Click a node or edge to inspect detailed information.</div>
        <div id="selectionMeta" class="detail-grid"></div>
        <div id="selectionActions" class="selection-actions">
          <button id="selectionOpenContentBtn" type="button">Open content</button>
          <button id="selectionCenterBtn" type="button">Center node</button>
          <button id="selectionPinBtn" type="button">Pin node</button>
          <button id="selectionFocusBtn" type="button">1-hop focus</button>
        </div>
      </div>
      <div class="card">
        <h2>Selection details</h2>
        <div id="selectionDetails" class="detail-list"></div>
      </div>
      <div class="card">
        <h2>Neighbors</h2>
        <div id="neighbors" class="detail-list"></div>
      </div>
      <div class="card">
        <h2>Incident edges</h2>
        <div id="incidentEdges" class="detail-list"></div>
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

  <div id="nodeContextMenu" class="context-menu" role="menu">
    <button id="openContentBtn" type="button" role="menuitem">Open node content</button>
  </div>

  <div id="contentModal" class="content-modal" aria-hidden="true">
    <div class="content-modal-card">
      <div class="content-modal-head">
        <div>
          <strong id="contentModalTitle">Node content</strong>
          <div id="contentModalSubtitle" class="muted" style="margin-top:6px;"></div>
          <div class="content-modal-actions">
            <button id="copyContentBtn" type="button" class="action-button">Copy content</button>
            <button id="toggleExpandContentBtn" type="button" class="action-button">Expand view</button>
            <button id="showPathBtn" type="button" class="action-button">Show file path</button>
            <span id="copyFeedback" class="copy-feedback"></span>
          </div>
        </div>
        <button id="closeContentModalBtn" type="button" class="close-button">Close</button>
      </div>
      <div class="content-modal-body">
        <div id="contentModalMeta" class="detail-grid" style="margin:16px 0;"></div>
        <div class="content-frame">
          <div class="content-toolbar">
            <span id="contentLanguagePill" class="badge language-pill">text</span>
            <span id="contentKindPill" class="badge">node</span>
          </div>
          <pre id="contentModalBody" class="content-view code-view"></pre>
        </div>
      </div>
    </div>
  </div>

  <script>
    const graph = {{.GraphJSON}};
    const nodeContents = {{.NodeContentsJSON}};

    const canvas = document.getElementById('graphSurface');
    const ctx = canvas.getContext('2d');
    const canvasEmpty = document.getElementById('canvasEmpty');
    const searchInput = document.getElementById('searchInput');
    const typeFilter = document.getElementById('typeFilter');
    const edgeFilter = document.getElementById('edgeFilter');
    const clusterMode = document.getElementById('clusterMode');
    const focusDepth = document.getElementById('focusDepth');
    const documentMode = document.getElementById('documentMode');
    const hideSymbols = document.getElementById('hideSymbols');
    const showLabels = document.getElementById('showLabels');
    const fadeUnrelated = document.getElementById('fadeUnrelated');
    const fitViewBtn = document.getElementById('fitViewBtn');
    const centerSelectedBtn = document.getElementById('centerSelectedBtn');
    const resetSelectionBtn = document.getElementById('resetSelectionBtn');
    const legend = document.getElementById('legend');
    const searchResults = document.getElementById('searchResults');
    const resultCount = document.getElementById('resultCount');
    const analysisPanel = document.getElementById('analysisPanel');
    const metrics = document.getElementById('metrics');
    const focusChips = document.getElementById('focusChips');
    const selectionSummary = document.getElementById('selectionSummary');
    const selectionMeta = document.getElementById('selectionMeta');
    const selectionActions = document.getElementById('selectionActions');
    const selectionOpenContentBtn = document.getElementById('selectionOpenContentBtn');
    const selectionCenterBtn = document.getElementById('selectionCenterBtn');
    const selectionPinBtn = document.getElementById('selectionPinBtn');
    const selectionFocusBtn = document.getElementById('selectionFocusBtn');
    const selectionDetails = document.getElementById('selectionDetails');
    const neighbors = document.getElementById('neighbors');
    const incidentEdges = document.getElementById('incidentEdges');
    const rawPayload = document.getElementById('rawPayload');
    const hoverCard = document.getElementById('hoverCard');
    const nodeContextMenu = document.getElementById('nodeContextMenu');
    const openContentBtn = document.getElementById('openContentBtn');
    const contentModal = document.getElementById('contentModal');
    const contentModalTitle = document.getElementById('contentModalTitle');
    const contentModalSubtitle = document.getElementById('contentModalSubtitle');
    const contentModalMeta = document.getElementById('contentModalMeta');
    const contentModalBody = document.getElementById('contentModalBody');
    const copyContentBtn = document.getElementById('copyContentBtn');
    const toggleExpandContentBtn = document.getElementById('toggleExpandContentBtn');
    const showPathBtn = document.getElementById('showPathBtn');
    const copyFeedback = document.getElementById('copyFeedback');
    const contentLanguagePill = document.getElementById('contentLanguagePill');
    const contentKindPill = document.getElementById('contentKindPill');
    const closeContentModalBtn = document.getElementById('closeContentModalBtn');
    const minimapButton = document.getElementById('minimapButton');
    const minimapCanvas = document.getElementById('minimapCanvas');
    const minimapCtx = minimapCanvas.getContext('2d');

    rawPayload.textContent = JSON.stringify(graph, null, 2);

    const palette = {
      file: css('--file'),
      symbol: css('--symbol'),
      document: css('--document'),
      module: css('--module'),
      package: css('--package'),
      import: css('--import')
    };

    const state = {
      query: '',
      nodeType: '',
      edgeType: '',
      clusterMode: 'type',
      focusDepth: 'all',
      documentMode: false,
      hideSymbols: false,
      showLabels: true,
      fadeUnrelated: true,
      selected: null,
      hover: null,
      dragNodeId: null,
      marquee: null,
      panning: false,
      pointer: null,
      transform: { x: 0, y: 0, scale: 1 },
      canvasRect: null
    };

    let activeContentEntry = null;

    const nodes = graph.nodes.map(function(node, index) {
      return Object.assign({}, node, {
        index: index,
        radius: radiusFor(node.type),
        color: palette[node.type] || '#7aa1db',
        cluster: '',
        x: 0,
        y: 0,
        vx: 0,
        vy: 0,
        fx: null,
        fy: null
      });
    });
    const nodeById = new Map(nodes.map(function(node) { return [node.id, node]; }));
    const edges = graph.edges.filter(function(edge) {
      return nodeById.has(edge.source) && nodeById.has(edge.target);
    }).map(function(edge, index) {
      return Object.assign({}, edge, { index: index, key: edge.source + '|' + edge.target + '|' + edge.type + '|' + index });
    });
    const adjacency = new Map(nodes.map(function(node) { return [node.id, new Set()]; }));
    edges.forEach(function(edge) {
      adjacency.get(edge.source).add(edge.target);
      adjacency.get(edge.target).add(edge.source);
    });

    populateFilters();
    renderLegend();
    renderAnalysis();
    seedLayout();
    runLayout(140);
    bindEvents();
    resizeCanvas();
    render();

    function css(name) { return getComputedStyle(document.documentElement).getPropertyValue(name).trim(); }

    function radiusFor(type) {
      if (type === 'module') return 13;
      if (type === 'package') return 11;
      if (type === 'document') return 10;
      if (type === 'file') return 9;
      if (type === 'import') return 7;
      return 6;
    }

    function populateFilters() {
      unique(nodes.map(function(node) { return node.type; })).forEach(function(type) {
        const option = document.createElement('option');
        option.value = type;
        option.textContent = type;
        typeFilter.appendChild(option);
      });
      unique(edges.map(function(edge) { return edge.type; })).forEach(function(type) {
        const option = document.createElement('option');
        option.value = type;
        option.textContent = type;
        edgeFilter.appendChild(option);
      });
    }

    function unique(values) { return Array.from(new Set(values.filter(Boolean))).sort(); }

    function renderLegend() {
      const counts = countBy(nodes, function(node) { return node.type; });
      legend.innerHTML = '';
      Object.keys(counts).sort().forEach(function(type) {
        const item = document.createElement('div');
        item.className = 'legend-item';
        item.innerHTML = '<span class="swatch" style="background:' + (palette[type] || '#7aa1db') + '; color:' + (palette[type] || '#7aa1db') + '"></span><strong>' + escapeHTML(type) + '</strong><span class="muted">' + counts[type] + '</span>';
        legend.appendChild(item);
      });
    }

    function renderAnalysis() {
      const analysis = graph.analysis || {};
      analysisPanel.innerHTML = '';
      appendMetricList('Top imports', analysis.top_imports || []);
      appendMetricList('Most connected files', analysis.most_connected_files || []);
      appendMetricList('Bridge files', analysis.bridge_files || []);
      appendMetricList('Hotspot files', analysis.hotspot_files || []);
      appendTextList('Relation highlights', analysis.relation_highlights || []);
      appendReadingPaths('Reading paths', analysis.reading_paths || []);
      appendTextList('Recommended files', analysis.recommended_files || []);
      if (!analysisPanel.children.length) {
        analysisPanel.innerHTML = '<div class="muted">No graph analysis available.</div>';
      }
    }

    function appendMetricList(title, items) {
      if (!items || !items.length) return;
      const block = document.createElement('div');
      block.className = 'analysis-item';
      block.innerHTML = '<strong>' + escapeHTML(title) + '</strong>';
      items.forEach(function(item) {
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
      items.forEach(function(item) {
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
      items.forEach(function(item) {
        const row = document.createElement('div');
        row.className = 'muted';
        row.style.marginTop = '8px';
        const path = Array.isArray(item.path || item.Path) ? (item.path || item.Path).join(' → ') : '';
        row.innerHTML = '<div><span class="badge">' + escapeHTML(item.entry || item.Entry || '') + '</span></div><div style="margin-top:6px;">' + escapeHTML(path) + '</div>' + ((item.reason || item.Reason) ? '<div style="margin-top:4px;">' + escapeHTML(item.reason || item.Reason) + '</div>' : '');
        block.appendChild(row);
      });
      analysisPanel.appendChild(block);
    }

    function seedLayout() {
      const groups = buildGroups(nodes, state.clusterMode);
      const centers = clusterCenters(Array.from(groups.keys()), state.clusterMode);
      nodes.forEach(function(node, index) {
        const cluster = clusterKey(node, state.clusterMode);
        const center = centers[cluster] || { x: 700, y: 450 };
        node.cluster = cluster;
        const angle = (index * 0.83) % (Math.PI * 2);
        const spread = 30 + (index % 12) * 10;
        node.x = center.x + Math.cos(angle) * spread;
        node.y = center.y + Math.sin(angle) * spread;
        node.fx = null;
        node.fy = null;
      });
      state.transform = { x: 0, y: 0, scale: 1 };
    }

    function runLayout(iterations) {
      for (let step = 0; step < iterations; step++) {
        nodes.forEach(function(node) {
          node.vx *= 0.88;
          node.vy *= 0.88;
        });
        for (let i = 0; i < nodes.length; i++) {
          const a = nodes[i];
          for (let j = i + 1; j < nodes.length; j++) {
            const b = nodes[j];
            let dx = a.x - b.x;
            let dy = a.y - b.y;
            let dist2 = dx * dx + dy * dy + 0.1;
            let force = 520 / dist2;
            if (a.cluster === b.cluster) force *= 1.15;
            const inv = 1 / Math.sqrt(dist2);
            dx *= inv; dy *= inv;
            a.vx += dx * force;
            a.vy += dy * force;
            b.vx -= dx * force;
            b.vy -= dy * force;
          }
        }
        edges.forEach(function(edge) {
          const s = nodeById.get(edge.source);
          const t = nodeById.get(edge.target);
          let dx = t.x - s.x;
          let dy = t.y - s.y;
          let dist = Math.sqrt(dx * dx + dy * dy) || 1;
          const desired = s.cluster === t.cluster ? 78 : 132;
          const force = (dist - desired) * 0.0048;
          dx /= dist; dy /= dist;
          s.vx += dx * force * 7;
          s.vy += dy * force * 7;
          t.vx -= dx * force * 7;
          t.vy -= dy * force * 7;
        });
        const centers = clusterCenters(unique(nodes.map(function(node) { return node.cluster; })), state.clusterMode);
        nodes.forEach(function(node) {
          const center = centers[node.cluster] || { x: 700, y: 450 };
          const gravity = state.clusterMode === 'none' ? 0.0007 : 0.0034;
          node.vx += (center.x - node.x) * gravity;
          node.vy += (center.y - node.y) * gravity;
          if (node.fx != null) node.x = node.fx; else node.x += node.vx;
          if (node.fy != null) node.y = node.fy; else node.y += node.vy;
          node.x = clamp(node.x, 80, 1320);
          node.y = clamp(node.y, 80, 820);
        });
      }
    }

    function buildGroups(list, mode) {
      const groups = new Map();
      list.forEach(function(node) {
        const key = clusterKey(node, mode);
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key).push(node);
      });
      return groups;
    }

    function clusterCenters(keys, mode) {
      const centers = {};
      keys.forEach(function(key, index) {
        const angle = (Math.PI * 2 * index) / Math.max(keys.length, 1);
        const radius = mode === 'none' ? 0 : 250;
        centers[key] = { x: 700 + Math.cos(angle) * radius, y: 450 + Math.sin(angle) * radius * 0.72 };
      });
      return centers;
    }

    function clusterKey(node, mode) {
      if (mode === 'none') return 'graph';
      if (mode === 'type') return node.type || 'unknown';
      if (node.type === 'module') return node.label || node.id;
      const file = node.file || '';
      if (!file) return node.type || 'unknown';
      const parts = file.split('/');
      if (parts.length <= 1) return 'root';
      return parts.slice(0, Math.min(2, parts.length - 1)).join('/');
    }

    function visibleNodes() {
      const query = state.query;
      let docScope = null;
      if (state.documentMode) {
        docScope = new Set();
        edges.forEach(function(edge) {
          const s = nodeById.get(edge.source);
          const t = nodeById.get(edge.target);
          if ((s && s.type === 'document') || (t && t.type === 'document')) {
            docScope.add(edge.source);
            docScope.add(edge.target);
          }
        });
      }
      let filtered = nodes.filter(function(node) {
        if (state.hideSymbols && node.type === 'symbol') return false;
        if (state.nodeType && node.type !== state.nodeType) return false;
        if (docScope && !docScope.has(node.id)) return false;
        if (!query) return true;
        const hay = [node.label, node.name, node.file, node.kind, node.id].filter(Boolean).join(' ').toLowerCase();
        return hay.includes(query) || (state.selected && state.selected.kind === 'node' && state.selected.id === node.id);
      });
      const focusIds = selectionScope(state.focusDepth);
      if (focusIds) {
        filtered = filtered.filter(function(node) { return focusIds.has(node.id); });
      }
      return filtered;
    }

    function visibleEdges(nodeSet) {
      return edges.filter(function(edge) {
        if (state.edgeType && edge.type !== state.edgeType) return false;
        return nodeSet.has(edge.source) && nodeSet.has(edge.target);
      });
    }

    function bindEvents() {
      searchInput.addEventListener('input', function(e) {
        state.query = e.target.value.trim().toLowerCase();
        renderSearchResults();
        render();
      });
      typeFilter.addEventListener('change', function(e) { state.nodeType = e.target.value; render(); });
      edgeFilter.addEventListener('change', function(e) { state.edgeType = e.target.value; render(); });
      focusDepth.addEventListener('change', function(e) { state.focusDepth = e.target.value; render(); });
      clusterMode.addEventListener('change', function(e) {
        state.clusterMode = e.target.value;
        seedLayout();
        runLayout(120);
        fitView();
        render();
      });
      documentMode.addEventListener('change', function(e) { state.documentMode = e.target.checked; render(); });
      hideSymbols.addEventListener('change', function(e) { state.hideSymbols = e.target.checked; render(); });
      showLabels.addEventListener('change', function(e) { state.showLabels = e.target.checked; render(); });
      fadeUnrelated.addEventListener('change', function(e) { state.fadeUnrelated = e.target.checked; render(); });
      fitViewBtn.addEventListener('click', function() { fitView(); render(); });
      centerSelectedBtn.addEventListener('click', function() {
        if (state.selected && state.selected.kind === 'node') {
          centerOnNode(nodeById.get(state.selected.id));
          render();
        }
      });
      resetSelectionBtn.addEventListener('click', function() { state.selected = null; render(); });
      window.addEventListener('resize', function() { resizeCanvas(); render(); });
      canvas.addEventListener('wheel', onWheel, { passive: false });
      canvas.addEventListener('mousedown', onMouseDown);
      canvas.addEventListener('dblclick', onDoubleClick);
      canvas.addEventListener('contextmenu', onCanvasContextMenu);
      window.addEventListener('mousemove', onMouseMove);
      window.addEventListener('mouseup', onMouseUp);
      canvas.addEventListener('mouseleave', function() { state.hover = null; hideHoverCard(); render(); });
      canvas.addEventListener('mousemove', onHover);
      canvas.addEventListener('click', onCanvasClick);
      minimapButton.addEventListener('click', onMinimapClick);
      openContentBtn.addEventListener('click', function() {
        if (state.selected && state.selected.kind === 'node') {
          openNodeContent(state.selected.id);
        }
        hideContextMenu();
      });
      selectionOpenContentBtn.addEventListener('click', function() {
        if (state.selected && state.selected.kind === 'node') {
          openNodeContent(state.selected.id);
        }
      });
      selectionCenterBtn.addEventListener('click', function() {
        if (state.selected && state.selected.kind === 'node') {
          centerOnNode(nodeById.get(state.selected.id));
          render();
        }
      });
      selectionPinBtn.addEventListener('click', function() {
        if (state.selected && state.selected.kind === 'node') {
          togglePinNode(state.selected.id);
          render();
        }
      });
      selectionFocusBtn.addEventListener('click', function() {
        if (state.focusDepth === '1') {
          state.focusDepth = '2';
          focusDepth.value = '2';
          selectionFocusBtn.textContent = 'Reset focus';
        } else if (state.focusDepth === '2') {
          state.focusDepth = 'all';
          focusDepth.value = 'all';
          selectionFocusBtn.textContent = '1-hop focus';
        } else {
          state.focusDepth = '1';
          focusDepth.value = '1';
          selectionFocusBtn.textContent = '2-hop focus';
        }
        render();
      });
      nodeContextMenu.addEventListener('click', function(event) { event.stopPropagation(); });
      nodeContextMenu.addEventListener('contextmenu', function(event) { event.preventDefault(); event.stopPropagation(); });
      copyContentBtn.addEventListener('click', copyActiveContent);
      toggleExpandContentBtn.addEventListener('click', toggleExpandContent);
      showPathBtn.addEventListener('click', showActivePath);
      closeContentModalBtn.addEventListener('click', closeContentModal);
      contentModal.addEventListener('click', function(event) {
        if (event.target === contentModal) closeContentModal();
      });
      window.addEventListener('keydown', function(event) {
        if (event.key === 'Escape') {
          hideContextMenu();
          closeContentModal();
        }
      });
      window.addEventListener('pointerdown', function(event) {
        if (!nodeContextMenu.contains(event.target)) {
          hideContextMenu();
        }
      });
    }

    function resizeCanvas() {
      const rect = canvas.getBoundingClientRect();
      state.canvasRect = rect;
      const ratio = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.floor(rect.width * ratio));
      canvas.height = Math.max(1, Math.floor(rect.height * ratio));
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
      minimapCanvas.width = 180 * ratio;
      minimapCanvas.height = 128 * ratio;
      minimapCtx.setTransform(ratio, 0, 0, ratio, 0, 0);
      fitView();
    }

    function onWheel(event) {
      event.preventDefault();
      const factor = event.deltaY < 0 ? 1.1 : 0.9;
      const world = screenToWorld(event.offsetX, event.offsetY);
      state.transform.scale = clamp(state.transform.scale * factor, 0.25, 3.5);
      state.transform.x = event.offsetX - world.x * state.transform.scale;
      state.transform.y = event.offsetY - world.y * state.transform.scale;
      render();
    }

    function onMouseDown(event) {
      if (event.button === 2) {
        state.dragNodeId = null;
        state.panning = false;
        state.pointer = null;
        canvas.classList.remove('dragging');
        return;
      }
      const hit = hitTest(event.offsetX, event.offsetY);
      state.pointer = { x: event.clientX, y: event.clientY };
      if (event.shiftKey) {
        state.marquee = { startX: event.offsetX, startY: event.offsetY, endX: event.offsetX, endY: event.offsetY };
        return;
      }
      if (hit && hit.kind === 'node') {
        state.dragNodeId = hit.id;
      } else {
        state.panning = true;
        canvas.classList.add('dragging');
      }
    }

    function onMouseMove(event) {
      if (!state.pointer) return;
      const dx = event.clientX - state.pointer.x;
      const dy = event.clientY - state.pointer.y;
      state.pointer = { x: event.clientX, y: event.clientY };
      if (state.marquee) {
        const rect = canvas.getBoundingClientRect();
        state.marquee.endX = event.clientX - rect.left;
        state.marquee.endY = event.clientY - rect.top;
        render();
        return;
      }
      if (state.dragNodeId) {
        const node = nodeById.get(state.dragNodeId);
        node.x += dx / state.transform.scale;
        node.y += dy / state.transform.scale;
        node.fx = node.x;
        node.fy = node.y;
        render();
      } else if (state.panning) {
        state.transform.x += dx;
        state.transform.y += dy;
        render();
      }
    }

    function onMouseUp() {
      if (state.marquee) {
        finalizeMarquee();
      }
      state.dragNodeId = null;
      state.panning = false;
      state.pointer = null;
      state.marquee = null;
      canvas.classList.remove('dragging');
    }

    function onDoubleClick(event) {
      const hit = hitTest(event.offsetX, event.offsetY);
      if (!hit || hit.kind !== 'node') return;
      togglePinNode(hit.id);
      const node = nodeById.get(hit.id);
      state.selected = { kind: 'node', id: node.id };
      render();
    }

    function onMinimapClick(event) {
      const rect = minimapCanvas.getBoundingClientRect();
      const x = event.clientX - rect.left;
      const y = event.clientY - rect.top;
      recenterFromMinimap(x, y);
      render();
    }

    function onHover(event) {
      state.hover = hitTest(event.offsetX, event.offsetY);
      canvas.style.cursor = state.hover ? 'pointer' : (state.panning ? 'grabbing' : 'grab');
      renderHoverCard(event.offsetX, event.offsetY);
      render();
    }

    function onCanvasClick(event) {
      hideContextMenu();
      const hit = hitTest(event.offsetX, event.offsetY);
      state.selected = hit;
      render();
    }

    function onCanvasContextMenu(event) {
      event.preventDefault();
      event.stopPropagation();
      state.dragNodeId = null;
      state.panning = false;
      state.pointer = null;
      canvas.classList.remove('dragging');
      const hit = hitTest(event.offsetX, event.offsetY);
      if (!hit || hit.kind !== 'node') {
        hideContextMenu();
        return;
      }
      state.selected = { kind: 'node', id: hit.id };
      showContextMenu(event.clientX, event.clientY);
      render();
    }

    function fitView() {
      const activeNodes = visibleNodes();
      if (!activeNodes.length || !state.canvasRect) return;
      const width = state.canvasRect.width || 1;
      const height = state.canvasRect.height || 1;
      const bounds = activeNodes.reduce(function(acc, node) {
        acc.minX = Math.min(acc.minX, node.x - node.radius);
        acc.maxX = Math.max(acc.maxX, node.x + node.radius);
        acc.minY = Math.min(acc.minY, node.y - node.radius);
        acc.maxY = Math.max(acc.maxY, node.y + node.radius);
        return acc;
      }, { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
      const graphWidth = Math.max(220, bounds.maxX - bounds.minX);
      const graphHeight = Math.max(220, bounds.maxY - bounds.minY);
      const scale = clamp(Math.min((width * 0.84) / graphWidth, (height * 0.84) / graphHeight), 0.25, 2.1);
      state.transform.scale = scale;
      state.transform.x = width / 2 - ((bounds.minX + bounds.maxX) / 2) * scale;
      state.transform.y = height / 2 - ((bounds.minY + bounds.maxY) / 2) * scale;
    }

    function centerOnNode(node) {
      if (!node || !state.canvasRect) return;
      state.transform.x = state.canvasRect.width / 2 - node.x * state.transform.scale;
      state.transform.y = state.canvasRect.height / 2 - node.y * state.transform.scale;
    }

    function screenToWorld(x, y) {
      return { x: (x - state.transform.x) / state.transform.scale, y: (y - state.transform.y) / state.transform.scale };
    }

    function hitTest(x, y) {
      const world = screenToWorld(x, y);
      const activeNodes = visibleNodes();
      const activeNodeIds = new Set(activeNodes.map(function(node) { return node.id; }));
      for (let i = activeNodes.length - 1; i >= 0; i--) {
        const node = activeNodes[i];
        if (distance(world.x, world.y, node.x, node.y) <= node.radius + 4 / state.transform.scale) {
          return { kind: 'node', id: node.id };
        }
      }
      const activeEdges = visibleEdges(activeNodeIds);
      for (let i = activeEdges.length - 1; i >= 0; i--) {
        const edge = activeEdges[i];
        const s = nodeById.get(edge.source);
        const t = nodeById.get(edge.target);
        if (pointToSegmentDistance(world.x, world.y, s.x, s.y, t.x, t.y) <= Math.max(5, 8 / state.transform.scale)) {
          return { kind: 'edge', key: edge.key };
        }
      }
      return null;
    }

    function render() {
      const activeNodes = visibleNodes();
      const nodeSet = new Set(activeNodes.map(function(node) { return node.id; }));
      const activeEdges = visibleEdges(nodeSet);
      canvasEmpty.style.display = activeNodes.length ? 'none' : 'flex';
      renderMetrics(activeNodes, activeEdges);
      renderFocusChips(activeNodes, activeEdges);
      renderSearchResults();
      draw(activeNodes, activeEdges);
      drawMinimap(activeNodes, activeEdges);
      renderSelection(activeNodes, activeEdges);
    }

    function draw(activeNodes, activeEdges) {
      const width = state.canvasRect ? state.canvasRect.width : canvas.clientWidth;
      const height = state.canvasRect ? state.canvasRect.height : canvas.clientHeight;
      ctx.clearRect(0, 0, width, height);
      ctx.save();
      ctx.translate(state.transform.x, state.transform.y);
      ctx.scale(state.transform.scale, state.transform.scale);
      drawClusters(activeNodes);
      drawEdges(activeEdges);
      drawNodes(activeNodes);
      if (state.showLabels) drawLabels(activeNodes);
      ctx.restore();
      drawMarquee();
    }

    function drawClusters(activeNodes) {
      if (state.clusterMode === 'none') return;
      const groups = buildGroups(activeNodes, state.clusterMode);
      groups.forEach(function(list, key) {
        if (!list.length) return;
        const bounds = list.reduce(function(acc, node) {
          acc.minX = Math.min(acc.minX, node.x);
          acc.maxX = Math.max(acc.maxX, node.x);
          acc.minY = Math.min(acc.minY, node.y);
          acc.maxY = Math.max(acc.maxY, node.y);
          return acc;
        }, { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
        const cx = (bounds.minX + bounds.maxX) / 2;
        const cy = (bounds.minY + bounds.maxY) / 2;
        const rx = Math.max(88, (bounds.maxX - bounds.minX) / 2 + 58);
        const ry = Math.max(72, (bounds.maxY - bounds.minY) / 2 + 46);
        ctx.save();
        ctx.strokeStyle = 'rgba(131,177,237,0.12)';
        ctx.fillStyle = 'rgba(255,255,255,0.015)';
        ctx.setLineDash([8, 10]);
        ctx.beginPath();
        ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.fillStyle = 'rgba(234,242,255,0.7)';
        ctx.font = '600 14px ui-sans-serif';
        ctx.fillText(key, cx - rx + 16, cy - ry + 22);
        ctx.restore();
      });
    }

    function drawEdges(activeEdges) {
      const neighborhood = selectionNeighborhood();
      activeEdges.forEach(function(edge) {
        const s = nodeById.get(edge.source);
        const t = nodeById.get(edge.target);
        ctx.save();
        ctx.strokeStyle = edgeColor(edge.type);
        ctx.globalAlpha = edgeOpacity(edge, neighborhood);
        ctx.lineWidth = edgeLineWidth(edge);
        ctx.beginPath();
        ctx.moveTo(s.x, s.y);
        ctx.lineTo(t.x, t.y);
        ctx.stroke();
        if (isSelectedEdge(edge)) {
          ctx.strokeStyle = 'rgba(255,255,255,0.92)';
          ctx.globalAlpha = 0.95;
          ctx.lineWidth = edgeLineWidth(edge) + 1.2;
          ctx.stroke();
        }
        ctx.restore();
      });
    }

    function drawNodes(activeNodes) {
      const neighborhood = selectionNeighborhood();
      activeNodes.forEach(function(node) {
        ctx.save();
        ctx.globalAlpha = nodeOpacity(node, neighborhood);
        ctx.fillStyle = node.color;
        ctx.shadowColor = node.color;
        ctx.shadowBlur = isSelectedNode(node) ? 20 : 8;
        ctx.beginPath();
        ctx.arc(node.x, node.y, node.radius + (isSelectedNode(node) ? 3 : 0), 0, Math.PI * 2);
        ctx.fill();
        ctx.shadowBlur = 0;
        ctx.lineWidth = isSelectedNode(node) ? 2.8 : (isHoveredNode(node) ? 2 : 1.2);
        ctx.strokeStyle = isSelectedNode(node) ? '#ffffff' : (isHoveredNode(node) ? 'rgba(255,255,255,0.8)' : 'rgba(255,255,255,0.18)');
        ctx.stroke();
        if (node.fx != null || node.fy != null) {
          ctx.fillStyle = 'rgba(255,255,255,0.95)';
          ctx.beginPath();
          ctx.arc(node.x + node.radius * 0.55, node.y - node.radius * 0.55, 2.6, 0, Math.PI * 2);
          ctx.fill();
        }
        ctx.restore();
      });
    }

    function drawMarquee() {
      if (!state.marquee) return;
      const x = Math.min(state.marquee.startX, state.marquee.endX);
      const y = Math.min(state.marquee.startY, state.marquee.endY);
      const w = Math.abs(state.marquee.endX - state.marquee.startX);
      const h = Math.abs(state.marquee.endY - state.marquee.startY);
      ctx.save();
      ctx.fillStyle = 'rgba(89,216,255,0.12)';
      ctx.strokeStyle = 'rgba(89,216,255,0.76)';
      ctx.setLineDash([6, 6]);
      ctx.fillRect(x, y, w, h);
      ctx.strokeRect(x, y, w, h);
      ctx.restore();
    }

    function drawLabels(activeNodes) {
      const neighborhood = selectionNeighborhood();
      activeNodes.forEach(function(node) {
        if (activeNodes.length > 170 && node.type === 'symbol' && !isSelectedNode(node) && !neighborhood.has(node.id)) return;
        ctx.save();
        ctx.globalAlpha = nodeOpacity(node, neighborhood);
        ctx.fillStyle = 'rgba(234,242,255,0.92)';
        ctx.font = '11px ui-sans-serif';
        ctx.fillText(node.label || node.name || node.id, node.x + node.radius + 7, node.y + 4);
        ctx.restore();
      });
    }

    function renderMetrics(activeNodes, activeEdges) {
      const docs = activeNodes.filter(function(node) { return node.type === 'document'; }).length;
      metrics.innerHTML = [
        '<div class="metric">Visible nodes ' + activeNodes.length + '</div>',
        '<div class="metric">Visible edges ' + activeEdges.length + '</div>',
        '<div class="metric">Documents ' + docs + '</div>',
        '<div class="metric">Zoom ' + Math.round(state.transform.scale * 100) + '%</div>'
      ].join('');
    }

    function renderFocusChips(activeNodes, activeEdges) {
      const chips = [];
      if (state.selected) {
        chips.push('<div class="pill focus-tag">Selected ' + escapeHTML(selectionLabel()) + '</div>');
      }
      if (state.focusDepth !== 'all') chips.push('<div class="pill focus-tag">' + escapeHTML(state.focusDepth) + '-hop focus</div>');
      if (state.documentMode) chips.push('<div class="pill focus-tag">Document mode</div>');
      if (state.nodeType) chips.push('<div class="pill focus-tag">Node filter ' + escapeHTML(state.nodeType) + '</div>');
      if (state.edgeType) chips.push('<div class="pill focus-tag">Edge filter ' + escapeHTML(state.edgeType) + '</div>');
      if (!chips.length) chips.push('<div class="badge">Showing ' + activeNodes.length + ' nodes and ' + activeEdges.length + ' edges</div>');
      focusChips.innerHTML = chips.join('');
    }

    function renderSearchResults() {
      const matches = searchMatches();
      resultCount.textContent = matches.length ? matches.length + ' matching nodes' : 'Type to locate files, symbols, and documents.';
      searchResults.innerHTML = '';
      matches.slice(0, 24).forEach(function(node) {
        const item = document.createElement('div');
        item.className = 'result' + (isSelectedNode(node) ? ' active' : '');
        item.innerHTML = '<strong>' + escapeHTML(node.label || node.id) + '</strong><div class="muted">' + escapeHTML(node.type + (node.file ? ' · ' + node.file : '')) + '</div>';
        item.addEventListener('click', function() {
          state.selected = { kind: 'node', id: node.id };
          centerOnNode(node);
          render();
        });
        searchResults.appendChild(item);
      });
    }

    function renderSelection(activeNodes, activeEdges) {
      selectionMeta.innerHTML = '';
      selectionDetails.innerHTML = '';
      neighbors.innerHTML = '';
      incidentEdges.innerHTML = '';
      selectionActions.style.display = 'grid';
      if (!state.selected) {
        selectionSummary.textContent = 'Click a node or edge to inspect detailed information.';
        selectionActions.style.display = 'none';
        selectionDetails.innerHTML = '<div class="muted">No selection.</div>';
        neighbors.innerHTML = '<div class="muted">No selection.</div>';
        incidentEdges.innerHTML = '<div class="muted">No selection.</div>';
        return;
      }
      if (state.selected.kind === 'node') {
        const node = nodeById.get(state.selected.id);
        if (!node) return;
        selectionOpenContentBtn.disabled = false;
        selectionCenterBtn.disabled = false;
        selectionPinBtn.disabled = false;
        selectionPinBtn.textContent = (node.fx != null || node.fy != null) ? 'Unpin node' : 'Pin node';
        selectionFocusBtn.disabled = false;
        selectionFocusBtn.textContent = state.focusDepth === 'all' ? '1-hop focus' : (state.focusDepth === '1' ? '2-hop focus' : 'Reset focus');
        selectionSummary.innerHTML = '<div class="detail-title"><strong>' + escapeHTML(node.label || node.id) + '</strong><span class="badge">node</span></div><div class="muted" style="margin-top:6px;">' + escapeHTML(node.type) + '</div>';
        selectionMeta.innerHTML = nodeMetaBadges(node);
        appendDetail(selectionDetails, 'Node ID', node.id);
        appendDetail(selectionDetails, 'Label', node.label || node.id);
        appendDetail(selectionDetails, 'Type', node.type);
        appendDetail(selectionDetails, 'Kind', node.kind || '');
        appendDetail(selectionDetails, 'File', node.file || '');
        appendDetail(selectionDetails, 'Language', node.language || '');
        appendDetail(selectionDetails, 'Line', node.line || '');
        const neighborIds = Array.from(adjacency.get(node.id) || []).filter(function(id) { return activeNodes.some(function(n) { return n.id === id; }); });
        if (!neighborIds.length) {
          neighbors.innerHTML = '<div class="muted">No visible neighbors.</div>';
        } else {
          neighborIds.slice(0, 20).forEach(function(id) {
            const neighbor = nodeById.get(id);
            const item = document.createElement('div');
            item.className = 'detail-item';
            item.innerHTML = '<button type="button" class="detail-button">' + escapeHTML(neighbor.label || neighbor.id) + '</button><div class="muted" style="margin-top:6px;">' + escapeHTML(neighbor.type + (neighbor.file ? ' · ' + neighbor.file : '')) + '</div>';
            item.querySelector('button').addEventListener('click', function() {
              state.selected = { kind: 'node', id: neighbor.id };
              centerOnNode(neighbor);
              render();
            });
            neighbors.appendChild(item);
          });
        }
        const relatedEdges = activeEdges.filter(function(edge) { return edge.source === node.id || edge.target === node.id; });
        if (!relatedEdges.length) {
          incidentEdges.innerHTML = '<div class="muted">No visible incident edges.</div>';
        } else {
          relatedEdges.slice(0, 24).forEach(function(edge) {
            incidentEdges.appendChild(edgeCard(edge));
          });
        }
        return;
      }

      const edge = edges.find(function(item) { return item.key === state.selected.key; });
      if (!edge) return;
      selectionOpenContentBtn.disabled = true;
      selectionCenterBtn.disabled = true;
      selectionPinBtn.disabled = true;
      selectionPinBtn.textContent = 'Pin node';
      selectionFocusBtn.disabled = true;
      selectionFocusBtn.textContent = '1-hop focus';
      const source = nodeById.get(edge.source);
      const target = nodeById.get(edge.target);
      selectionSummary.innerHTML = '<div class="detail-title"><strong>' + escapeHTML(edge.type) + '</strong><span class="badge">edge</span></div><div class="muted" style="margin-top:6px;">' + escapeHTML((source.label || source.id) + ' → ' + (target.label || target.id)) + '</div>';
      selectionMeta.innerHTML = '<span class="badge">line ' + escapeHTML(String(edge.line || 0)) + '</span>';
      appendDetail(selectionDetails, 'Source', source.id);
      appendDetail(selectionDetails, 'Target', target.id);
      appendDetail(selectionDetails, 'Type', edge.type);
      appendDetail(selectionDetails, 'Evidence', edge.evidence || '');
      appendDetail(selectionDetails, 'Confidence', edge.confidence || '');
      neighbors.appendChild(nodeCard(source));
      neighbors.appendChild(nodeCard(target));
      incidentEdges.appendChild(edgeCard(edge));
    }

    function renderHoverCard(x, y) {
      if (!state.hover || !state.canvasRect) {
        hideHoverCard();
        return;
      }
      let html = '';
      if (state.hover.kind === 'node') {
        const node = nodeById.get(state.hover.id);
        html = '<strong>' + escapeHTML(node.label || node.id) + '</strong><div class="muted">' + escapeHTML(node.type + (node.file ? ' · ' + node.file : '')) + '</div>' + (node.kind ? '<div style="margin-top:6px;">kind: ' + escapeHTML(node.kind) + '</div>' : '');
      } else if (state.hover.kind === 'edge') {
        const edge = edges.find(function(item) { return item.key === state.hover.key; });
        if (edge) {
          const s = nodeById.get(edge.source);
          const t = nodeById.get(edge.target);
          html = '<strong>' + escapeHTML(edge.type) + '</strong><div class="muted">' + escapeHTML((s.label || s.id) + ' → ' + (t.label || t.id)) + '</div>' + (edge.evidence ? '<div style="margin-top:6px;">' + escapeHTML(edge.evidence) + '</div>' : '');
        }
      }
      if (!html) {
        hideHoverCard();
        return;
      }
      hoverCard.innerHTML = html;
      hoverCard.classList.add('visible');
      const left = Math.min(x + 18, state.canvasRect.width - 240);
      const top = Math.min(y + 18, state.canvasRect.height - 120);
      hoverCard.style.left = left + 'px';
      hoverCard.style.top = top + 'px';
    }

    function hideHoverCard() {
      hoverCard.classList.remove('visible');
    }

    function showContextMenu(clientX, clientY) {
      const left = Math.min(clientX, window.innerWidth - 220);
      const top = Math.min(clientY, window.innerHeight - 120);
      nodeContextMenu.style.left = left + 'px';
      nodeContextMenu.style.top = top + 'px';
      nodeContextMenu.classList.add('visible');
    }

    function hideContextMenu() {
      nodeContextMenu.classList.remove('visible');
    }

    function openNodeContent(nodeId) {
      const node = nodeById.get(nodeId);
      const entry = nodeContents[nodeId];
      activeContentEntry = entry || null;
      contentModalTitle.textContent = (entry && entry.title) || (node && (node.label || node.id)) || nodeId;
      contentModalSubtitle.textContent = (entry && entry.subtitle) || (node && node.file) || '';
      contentModalMeta.innerHTML = '';
      copyFeedback.textContent = '';
      if (node) {
        const badges = [
          node.type ? '<span class="badge">' + escapeHTML(node.type) + '</span>' : '',
          node.kind ? '<span class="badge">' + escapeHTML(node.kind) + '</span>' : '',
          node.language ? '<span class="badge">' + escapeHTML(String(node.language)) + '</span>' : '',
          node.line ? '<span class="badge">line ' + escapeHTML(String(node.line)) + '</span>' : ''
        ].join('');
        contentModalMeta.innerHTML = badges + ((entry && entry.truncated) ? '<span class="badge">truncated</span>' : '');
      }
      const language = (entry && entry.language) || (node && node.language) || 'text';
      contentLanguagePill.textContent = language || 'text';
      contentKindPill.textContent = (entry && entry.kind) || (node && node.type) || 'node';
      contentModalBody.classList.remove('expanded');
      toggleExpandContentBtn.textContent = 'Expand view';
      renderContentBody(entry && entry.content ? entry.content : 'No embedded content available for this node.', String(language || 'text'));
      contentModal.classList.add('visible');
      contentModal.setAttribute('aria-hidden', 'false');
    }

    function closeContentModal() {
      activeContentEntry = null;
      contentModal.classList.remove('visible');
      contentModal.setAttribute('aria-hidden', 'true');
    }

    function renderContentBody(content, language) {
      const normalized = content || '';
      const lang = String(language || 'text').toLowerCase();
      const isTextLike = lang === 'markdown' || lang === 'text' || lang === 'md' || lang === 'txt';
      contentModalBody.className = 'content-view ' + (isTextLike ? 'text-view' : 'code-view');
      if (isTextLike) {
        contentModalBody.textContent = normalized;
        return;
      }
      contentModalBody.innerHTML = highlightCode(normalized);
    }

    function highlightCode(text) {
      const escaped = escapeHTML(text);
      return escaped
        .replace(/(\/\/.*|#.*$)/gm, '<span class="tok-comment">$1</span>')
        .replace(/(&quot;[^&]*?&quot;|'[^']*')/g, '<span class="tok-string">$1</span>')
        .replace(/\b(package|import|func|type|struct|interface|return|if|else|for|range|switch|case|default|const|var|class|extends|implements|public|private|protected|async|await|def|from|export|let)\b/g, '<span class="tok-keyword">$1</span>')
        .replace(/\b([0-9]+)\b/g, '<span class="tok-number">$1</span>')
        .replace(/\b([A-Z][A-Za-z0-9_]+)\b/g, '<span class="tok-type">$1</span>')
        .replace(/\b([a-zA-Z_][a-zA-Z0-9_]*)\s*(?=\()/g, '<span class="tok-func">$1</span>')
        .replace(/([A-Za-z0-9_\-\/]+\.[A-Za-z0-9_]+)/g, '<span class="tok-path">$1</span>');
    }

    async function copyActiveContent() {
      const content = activeContentEntry && activeContentEntry.content ? activeContentEntry.content : contentModalBody.textContent;
      try {
        await navigator.clipboard.writeText(content || '');
        copyFeedback.textContent = 'Copied';
        setTimeout(function() { copyFeedback.textContent = ''; }, 1200);
      } catch (error) {
        copyFeedback.textContent = 'Copy failed';
      }
    }

    function toggleExpandContent() {
      contentModalBody.classList.toggle('expanded');
      toggleExpandContentBtn.textContent = contentModalBody.classList.contains('expanded') ? 'Collapse view' : 'Expand view';
    }

    function showActivePath() {
      const path = activeContentEntry && activeContentEntry.path ? activeContentEntry.path : contentModalSubtitle.textContent;
      if (!path) return;
      contentModalMeta.innerHTML += '<span class="badge">path ' + escapeHTML(path) + '</span>';
    }

    function drawMinimap(activeNodes, activeEdges) {
      const width = 180;
      const height = 128;
      minimapCtx.clearRect(0, 0, width, height);
      if (!activeNodes.length) return;
      const bounds = activeNodes.reduce(function(acc, node) {
        acc.minX = Math.min(acc.minX, node.x);
        acc.maxX = Math.max(acc.maxX, node.x);
        acc.minY = Math.min(acc.minY, node.y);
        acc.maxY = Math.max(acc.maxY, node.y);
        return acc;
      }, { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
      const graphWidth = Math.max(1, bounds.maxX - bounds.minX);
      const graphHeight = Math.max(1, bounds.maxY - bounds.minY);
      const scale = Math.min((width - 16) / graphWidth, (height - 16) / graphHeight);
      const offsetX = (width - graphWidth * scale) / 2 - bounds.minX * scale;
      const offsetY = (height - graphHeight * scale) / 2 - bounds.minY * scale;
      minimapCtx.save();
      minimapCtx.fillStyle = 'rgba(7,14,25,0.9)';
      minimapCtx.fillRect(0, 0, width, height);
      activeEdges.forEach(function(edge) {
        const s = nodeById.get(edge.source);
        const t = nodeById.get(edge.target);
        minimapCtx.strokeStyle = edgeColor(edge.type);
        minimapCtx.globalAlpha = 0.35;
        minimapCtx.lineWidth = 1;
        minimapCtx.beginPath();
        minimapCtx.moveTo(s.x * scale + offsetX, s.y * scale + offsetY);
        minimapCtx.lineTo(t.x * scale + offsetX, t.y * scale + offsetY);
        minimapCtx.stroke();
      });
      activeNodes.forEach(function(node) {
        minimapCtx.fillStyle = node.color;
        minimapCtx.globalAlpha = isSelectedNode(node) ? 1 : 0.9;
        minimapCtx.beginPath();
        minimapCtx.arc(node.x * scale + offsetX, node.y * scale + offsetY, Math.max(2, node.radius * 0.35), 0, Math.PI * 2);
        minimapCtx.fill();
      });
      if (state.canvasRect) {
        const viewLeft = (-state.transform.x) / state.transform.scale;
        const viewTop = (-state.transform.y) / state.transform.scale;
        const viewWidth = state.canvasRect.width / state.transform.scale;
        const viewHeight = state.canvasRect.height / state.transform.scale;
        minimapCtx.strokeStyle = 'rgba(255,255,255,0.8)';
        minimapCtx.lineWidth = 1.2;
        minimapCtx.globalAlpha = 0.9;
        minimapCtx.strokeRect(viewLeft * scale + offsetX, viewTop * scale + offsetY, viewWidth * scale, viewHeight * scale);
      }
      minimapCtx.restore();
    }

    function finalizeMarquee() {
      const x1 = Math.min(state.marquee.startX, state.marquee.endX);
      const y1 = Math.min(state.marquee.startY, state.marquee.endY);
      const x2 = Math.max(state.marquee.startX, state.marquee.endX);
      const y2 = Math.max(state.marquee.startY, state.marquee.endY);
      if (x2 - x1 < 16 || y2 - y1 < 16) return;
      const a = screenToWorld(x1, y1);
      const b = screenToWorld(x2, y2);
      const worldMinX = Math.min(a.x, b.x);
      const worldMaxX = Math.max(a.x, b.x);
      const worldMinY = Math.min(a.y, b.y);
      const worldMaxY = Math.max(a.y, b.y);
      const worldWidth = Math.max(1, worldMaxX - worldMinX);
      const worldHeight = Math.max(1, worldMaxY - worldMinY);
      const scale = clamp(Math.min(state.canvasRect.width / worldWidth, state.canvasRect.height / worldHeight) * 0.92, 0.25, 3.5);
      state.transform.scale = scale;
      state.transform.x = state.canvasRect.width / 2 - ((worldMinX + worldMaxX) / 2) * scale;
      state.transform.y = state.canvasRect.height / 2 - ((worldMinY + worldMaxY) / 2) * scale;
    }

    function recenterFromMinimap(x, y) {
      const activeNodes = visibleNodes();
      if (!activeNodes.length) return;
      const width = 180;
      const height = 128;
      const bounds = activeNodes.reduce(function(acc, node) {
        acc.minX = Math.min(acc.minX, node.x);
        acc.maxX = Math.max(acc.maxX, node.x);
        acc.minY = Math.min(acc.minY, node.y);
        acc.maxY = Math.max(acc.maxY, node.y);
        return acc;
      }, { minX: Infinity, maxX: -Infinity, minY: Infinity, maxY: -Infinity });
      const graphWidth = Math.max(1, bounds.maxX - bounds.minX);
      const graphHeight = Math.max(1, bounds.maxY - bounds.minY);
      const scale = Math.min((width - 16) / graphWidth, (height - 16) / graphHeight);
      const offsetX = (width - graphWidth * scale) / 2 - bounds.minX * scale;
      const offsetY = (height - graphHeight * scale) / 2 - bounds.minY * scale;
      const worldX = (x - offsetX) / scale;
      const worldY = (y - offsetY) / scale;
      state.transform.x = state.canvasRect.width / 2 - worldX * state.transform.scale;
      state.transform.y = state.canvasRect.height / 2 - worldY * state.transform.scale;
    }

    function nodeMetaBadges(node) {
      return [
        node.file ? '<span class="badge">file ' + escapeHTML(node.file) + '</span>' : '',
        node.kind ? '<span class="badge">kind ' + escapeHTML(node.kind) + '</span>' : '',
        node.language ? '<span class="badge">language ' + escapeHTML(node.language) + '</span>' : '',
        node.line ? '<span class="badge">line ' + escapeHTML(String(node.line)) + '</span>' : ''
      ].join('');
    }

    function appendDetail(container, label, value) {
      if (value === '' || value == null) return;
      const row = document.createElement('div');
      row.className = 'detail-item';
      row.innerHTML = '<strong>' + escapeHTML(label) + '</strong><div class="muted" style="margin-top:6px;">' + escapeHTML(String(value)) + '</div>';
      container.appendChild(row);
    }

    function nodeCard(node) {
      const item = document.createElement('div');
      item.className = 'detail-item';
      item.innerHTML = '<button type="button" class="detail-button">' + escapeHTML(node.label || node.id) + '</button><div class="muted" style="margin-top:6px;">' + escapeHTML(node.type + (node.file ? ' · ' + node.file : '')) + '</div>';
      item.querySelector('button').addEventListener('click', function() {
        state.selected = { kind: 'node', id: node.id };
        centerOnNode(node);
        render();
      });
      return item;
    }

    function edgeCard(edge) {
      const item = document.createElement('div');
      item.className = 'detail-item';
      const s = nodeById.get(edge.source);
      const t = nodeById.get(edge.target);
      item.innerHTML = '<button type="button" class="detail-button">' + escapeHTML(edge.type + ': ' + (s.label || s.id) + ' → ' + (t.label || t.id)) + '</button><div class="muted" style="margin-top:6px;">' + escapeHTML(edge.evidence || 'no evidence') + '</div>';
      item.querySelector('button').addEventListener('click', function() {
        state.selected = { kind: 'edge', key: edge.key };
        render();
      });
      return item;
    }

    function searchMatches() {
      if (!state.query) return nodes.slice(0, 18);
      return nodes.filter(function(node) {
        const hay = [node.label, node.name, node.file, node.kind, node.id].filter(Boolean).join(' ').toLowerCase();
        return hay.includes(state.query);
      }).sort(function(a, b) { return matchScore(b) - matchScore(a); });
    }

    function matchScore(node) {
      const query = state.query;
      const label = (node.label || '').toLowerCase();
      const text = [node.label, node.name, node.file, node.id].filter(Boolean).join(' ').toLowerCase();
      if (label === query) return 100;
      if (text === query) return 96;
      if (label.startsWith(query)) return 82;
      if (text.startsWith(query)) return 72;
      if (text.includes(query)) return 60;
      return 0;
    }

    function togglePinNode(nodeId) {
      const node = nodeById.get(nodeId);
      if (!node) return;
      if (node.fx != null || node.fy != null) {
        node.fx = null;
        node.fy = null;
      } else {
        node.fx = node.x;
        node.fy = node.y;
      }
    }

    function selectionScope(depth) {
      if (depth === 'all' || !state.selected || state.selected.kind !== 'node') return null;
      const maxDepth = Number(depth);
      if (!Number.isFinite(maxDepth)) return null;
      const seen = new Set([state.selected.id]);
      let frontier = [state.selected.id];
      for (let level = 0; level < maxDepth; level++) {
        const next = [];
        frontier.forEach(function(id) {
          (adjacency.get(id) || new Set()).forEach(function(neighbor) {
            if (!seen.has(neighbor)) {
              seen.add(neighbor);
              next.push(neighbor);
            }
          });
        });
        frontier = next;
        if (!frontier.length) break;
      }
      return seen;
    }

    function selectionNeighborhood() {
      const set = new Set();
      if (!state.selected) return set;
      if (state.selected.kind === 'node' && adjacency.has(state.selected.id)) {
        set.add(state.selected.id);
        adjacency.get(state.selected.id).forEach(function(id) { set.add(id); });
        return set;
      }
      if (state.selected.kind === 'edge') {
        const edge = edges.find(function(item) { return item.key === state.selected.key; });
        if (edge) {
          set.add(edge.source);
          set.add(edge.target);
        }
      }
      return set;
    }

    function edgeColor(type) {
      if (String(type).indexOf('mentions_') === 0) return 'rgba(134,243,182,0.58)';
      if (type === 'imports') return 'rgba(255,157,118,0.44)';
      if (type === 'defines') return 'rgba(255,209,102,0.48)';
      if (type === 'belongs_to' || type === 'declares_package' || type === 'describes') return 'rgba(183,156,255,0.42)';
      return 'rgba(122,161,219,0.28)';
    }

    function edgeLineWidth(edge) {
      if (isSelectedEdge(edge)) return 3.4;
      if (String(edge.type).indexOf('mentions_') === 0) return 2.1;
      return 1.3;
    }

    function edgeOpacity(edge, neighborhood) {
      if (!state.selected || !state.fadeUnrelated) return 0.82;
      if (isSelectedEdge(edge)) return 1;
      if (state.selected.kind === 'node' && (edge.source === state.selected.id || edge.target === state.selected.id)) return 0.98;
      if (neighborhood.has(edge.source) && neighborhood.has(edge.target)) return 0.4;
      return 0.08;
    }

    function nodeOpacity(node, neighborhood) {
      if (!state.selected || !state.fadeUnrelated) return 0.96;
      if (neighborhood.has(node.id)) return 1;
      return 0.12;
    }

    function isSelectedNode(node) { return state.selected && state.selected.kind === 'node' && state.selected.id === node.id; }
    function isHoveredNode(node) { return state.hover && state.hover.kind === 'node' && state.hover.id === node.id; }
    function isSelectedEdge(edge) { return state.selected && state.selected.kind === 'edge' && state.selected.key === edge.key; }

    function selectionLabel() {
      if (!state.selected) return '';
      if (state.selected.kind === 'node') {
        const node = nodeById.get(state.selected.id);
        return node ? (node.label || node.id) : state.selected.id;
      }
      const edge = edges.find(function(item) { return item.key === state.selected.key; });
      return edge ? edge.type : 'edge';
    }

    function countBy(list, pick) {
      return list.reduce(function(acc, item) {
        const key = pick(item);
        acc[key] = (acc[key] || 0) + 1;
        return acc;
      }, {});
    }

    function distance(ax, ay, bx, by) {
      const dx = ax - bx;
      const dy = ay - by;
      return Math.sqrt(dx * dx + dy * dy);
    }

    function pointToSegmentDistance(px, py, x1, y1, x2, y2) {
      const dx = x2 - x1;
      const dy = y2 - y1;
      const l2 = dx * dx + dy * dy;
      if (l2 === 0) return distance(px, py, x1, y1);
      let t = ((px - x1) * dx + (py - y1) * dy) / l2;
      t = Math.max(0, Math.min(1, t));
      return distance(px, py, x1 + t * dx, y1 + t * dy);
    }

    function clamp(value, min, max) { return Math.max(min, Math.min(max, value)); }

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
