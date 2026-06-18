package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"
	"time"

	helix "github.com/helixdb/helix-db/sdks/go"

	"github.com/sjzsdu/code-context/internal/api"
)

const HelixSchemaVersion = "helix.schema.v1.code-context"

const (
	helixFileLabel           = "CodeContextFile"
	helixSymbolLabel         = "CodeContextSymbol"
	helixImportLabel         = "CodeContextImport"
	helixCallLabel           = "CodeContextCall"
	helixRouteLabel          = "CodeContextRoute"
	helixDocumentLabel       = "CodeContextDocument"
	helixDocumentLinkLabel   = "CodeContextDocumentLink"
	helixEmbeddingChunkLabel = "CodeContextEmbeddingChunk"

	helixDefinesEdge       = "DEFINES"
	helixImportsEdge       = "IMPORTS"
	helixRecordsCallEdge   = "RECORDS_CALL"
	helixDeclaresRouteEdge = "DECLARES_ROUTE"
	helixDocumentLinkEdge  = "HAS_DOCUMENT_LINK"
)

type helixStore struct {
	client             helixExecutor
	projectID          string
	requestTimeout     time.Duration
	readRetryAttempts  int
	readRetryBackoff   time.Duration
	writeRetryAttempts int
	writeRetryBackoff  time.Duration
}

type helixExecutor interface {
	Exec(ctx context.Context, req helix.Request, out any, opts ...helix.ExecOption) error
}

func NewHelixStore(opts HelixOptions) (Store, error) {
	if opts.Timeout < 0 {
		return nil, fmt.Errorf("helix timeout must be non-negative")
	}
	if opts.ReadRetryAttempts < 0 {
		return nil, fmt.Errorf("helix read retry attempts must be non-negative")
	}
	if opts.ReadRetryBackoff < 0 {
		return nil, fmt.Errorf("helix read retry backoff must be non-negative")
	}
	if opts.WriteRetryAttempts < 0 {
		return nil, fmt.Errorf("helix write retry attempts must be non-negative")
	}
	if opts.WriteRetryBackoff < 0 {
		return nil, fmt.Errorf("helix write retry backoff must be non-negative")
	}
	apiKey := opts.APIKey
	if apiKey == "" && opts.APIKeyEnv != "" {
		apiKey = os.Getenv(opts.APIKeyEnv)
	}
	var clientOpts []helix.ClientOption
	if apiKey != "" {
		clientOpts = append(clientOpts, helix.WithAPIKey(apiKey))
	}
	if opts.Timeout > 0 {
		clientOpts = append(clientOpts, helix.WithHTTPClient(&http.Client{Timeout: opts.Timeout}))
	}
	client, err := helix.NewClient(strings.TrimSpace(opts.URL), clientOpts...)
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(opts.ProjectID)
	if projectID == "" {
		projectID = "default"
	}
	readRetryAttempts := opts.ReadRetryAttempts
	if readRetryAttempts <= 0 {
		readRetryAttempts = 2
	}
	readRetryBackoff := opts.ReadRetryBackoff
	if readRetryBackoff <= 0 {
		readRetryBackoff = 50 * time.Millisecond
	}
	writeRetryAttempts := opts.WriteRetryAttempts
	if writeRetryAttempts <= 0 {
		writeRetryAttempts = 3
	}
	writeRetryBackoff := opts.WriteRetryBackoff
	if writeRetryBackoff <= 0 {
		writeRetryBackoff = 50 * time.Millisecond
	}
	return &helixStore{
		client:             client,
		projectID:          projectID,
		requestTimeout:     opts.Timeout,
		readRetryAttempts:  readRetryAttempts,
		readRetryBackoff:   readRetryBackoff,
		writeRetryAttempts: writeRetryAttempts,
		writeRetryBackoff:  writeRetryBackoff,
	}, nil
}

func (s *helixStore) Init(ctx context.Context) error {
	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_init")
		q.VarAs("file_key", helix.G().CreateIndexIfNotExists(helix.NodeUniqueEqualityIndex(helixFileLabel, "key")))
		q.VarAs("file_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixFileLabel, "project_id")))
		q.VarAs("file_path", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixFileLabel, "path")))
		q.VarAs("file_language", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixFileLabel, "language")))
		q.VarAs("symbol_key", helix.G().CreateIndexIfNotExists(helix.NodeUniqueEqualityIndex(helixSymbolLabel, "key")))
		q.VarAs("symbol_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixSymbolLabel, "project_id")))
		q.VarAs("symbol_file", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixSymbolLabel, "file_path")))
		q.VarAs("symbol_name", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixSymbolLabel, "name")))
		q.VarAs("symbol_kind", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixSymbolLabel, "kind")))
		q.VarAs("symbol_text", helix.G().CreateTextIndexNodes(helixSymbolLabel, "search_text"))
		q.VarAs("import_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixImportLabel, "project_id")))
		q.VarAs("import_file", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixImportLabel, "file_path")))
		q.VarAs("import_source", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixImportLabel, "source")))
		q.VarAs("call_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixCallLabel, "project_id")))
		q.VarAs("call_file", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixCallLabel, "file_path")))
		q.VarAs("call_from", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixCallLabel, "from_symbol")))
		q.VarAs("call_to", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixCallLabel, "to_name")))
		q.VarAs("route_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixRouteLabel, "project_id")))
		q.VarAs("route_file", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixRouteLabel, "file_path")))
		q.VarAs("route_path", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixRouteLabel, "path")))
		q.VarAs("route_handler", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixRouteLabel, "handler")))
		q.VarAs("document_key", helix.G().CreateIndexIfNotExists(helix.NodeUniqueEqualityIndex(helixDocumentLabel, "key")))
		q.VarAs("document_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixDocumentLabel, "project_id")))
		q.VarAs("document_path", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixDocumentLabel, "path")))
		q.VarAs("document_text", helix.G().CreateTextIndexNodes(helixDocumentLabel, "search_text"))
		q.VarAs("document_link_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixDocumentLinkLabel, "project_id")))
		q.VarAs("document_link_doc", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixDocumentLinkLabel, "document_path")))
		q.VarAs("document_link_target", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixDocumentLinkLabel, "target_key")))
		q.VarAs("embedding_chunk_key", helix.G().CreateIndexIfNotExists(helix.NodeUniqueEqualityIndex(helixEmbeddingChunkLabel, "key")))
		q.VarAs("embedding_chunk_cache_key", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixEmbeddingChunkLabel, "cache_key")))
		q.VarAs("embedding_chunk_project", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixEmbeddingChunkLabel, "project_id")))
		q.VarAs("embedding_chunk_model", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixEmbeddingChunkLabel, "model")))
		q.VarAs("embedding_chunk_target", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixEmbeddingChunkLabel, "target_key")))
		q.VarAs("embedding_chunk_target_path", helix.G().CreateIndexIfNotExists(helix.NodeEqualityIndex(helixEmbeddingChunkLabel, "target_path")))
		return q.Returning()
	}, nil)
}

func (s *helixStore) UpsertFile(ctx context.Context, f *api.FileInfo) (int64, error) {
	if f == nil {
		return 0, fmt.Errorf("file is required")
	}
	now := time.Now().Unix()
	var out struct {
		Updated helixRows[idRow] `json:"updated"`
		Created helixRows[idRow] `json:"created"`
	}
	err := s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_upsert_file")
		projectID := q.ParamString("project_id", s.projectID)
		key := q.ParamString("key", helixKey(s.projectID, f.Path))
		path := q.ParamString("path", f.Path)
		language := q.ParamString("language", string(f.Language))
		contentHash := q.ParamString("content_hash", f.ContentHash)
		size := q.ParamI64("size", f.Size)
		indexedAt := q.ParamI64("indexed_at", now)
		q.VarAs("existing", helix.G().NWithLabel(helixFileLabel).Where(helix.PredEq("key", key)))
		q.VarAsIf("updated", helix.VarNotEmpty("existing"), helix.G().N(helix.NodeVar("existing")).
			SetProperty("project_id", projectID).
			SetProperty("language", language).
			SetProperty("content_hash", contentHash).
			SetProperty("size", size).
			SetProperty("indexed_at", indexedAt).
			Project(helix.ProjectPropAs("$id", "id")))
		q.VarAsIf("created", helix.VarEmpty("existing"), helix.G().AddN(helixFileLabel, helix.Props{
			helix.Prop("key", key),
			helix.Prop("project_id", projectID),
			helix.Prop("path", path),
			helix.Prop("language", language),
			helix.Prop("content_hash", contentHash),
			helix.Prop("size", size),
			helix.Prop("indexed_at", indexedAt),
		}).Project(helix.ProjectPropAs("$id", "id")))
		return q.Returning("updated", "created")
	}, &out)
	if err != nil {
		return 0, err
	}
	return firstID(out.Updated.Properties, out.Created.Properties), nil
}

func (s *helixStore) ReplaceFileIndex(ctx context.Context, idx FileIndex) (int64, error) {
	if idx.File == nil {
		return 0, fmt.Errorf("file index file is required")
	}
	now := time.Now().Unix()
	symbols := symbolParamRows(s.projectID, idx.File.Path, idx.Symbols)
	imports := importParamRows(s.projectID, idx.File.Path, idx.Imports)
	calls := callParamRows(s.projectID, idx.File.Path, idx.Calls)
	routes := routeParamRows(s.projectID, idx.File.Path, idx.Routes)
	var out struct {
		FileID helixRows[idRow] `json:"file_id"`
	}
	err := s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_replace_file_index")
		projectID := q.ParamString("project_id", s.projectID)
		key := q.ParamString("key", helixKey(s.projectID, idx.File.Path))
		path := q.ParamString("path", idx.File.Path)
		language := q.ParamString("language", string(idx.File.Language))
		contentHash := q.ParamString("content_hash", idx.File.ContentHash)
		size := q.ParamI64("size", idx.File.Size)
		indexedAt := q.ParamI64("indexed_at", now)
		q.ParamArray("symbols", symbols, helix.ParamTypeObject())
		q.ParamArray("imports", imports, helix.ParamTypeObject())
		q.ParamArray("calls", calls, helix.ParamTypeObject())
		q.ParamArray("routes", routes, helix.ParamTypeObject())
		q.VarAs("existing", helix.G().NWithLabel(helixFileLabel).Where(helix.PredEq("key", key)))
		q.VarAs("drop_symbols", helix.G().N(helix.NodeVar("existing")).Out(helixDefinesEdge).Drop().Count())
		q.VarAs("drop_imports", helix.G().N(helix.NodeVar("existing")).Out(helixImportsEdge).Drop().Count())
		q.VarAs("drop_calls", helix.G().N(helix.NodeVar("existing")).Out(helixRecordsCallEdge).Drop().Count())
		q.VarAs("drop_routes", helix.G().N(helix.NodeVar("existing")).Out(helixDeclaresRouteEdge).Drop().Count())
		q.VarAs("drop_embedding_chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).
			Where(helix.PredEq("project_id", projectID)).
			Where(helix.PredEq("target_path", path)).
			Drop().Count())
		q.VarAs("drop_file", helix.G().N(helix.NodeVar("existing")).Drop().Count())
		q.VarAs("file", helix.G().AddN(helixFileLabel, helix.Props{
			helix.Prop("key", key),
			helix.Prop("project_id", projectID),
			helix.Prop("path", path),
			helix.Prop("language", language),
			helix.Prop("content_hash", contentHash),
			helix.Prop("size", size),
			helix.Prop("indexed_at", indexedAt),
		}))
		q.ForEachParam("symbols", symbolWriteBatch(helix.NodeVar("file")))
		q.ForEachParam("imports", importWriteBatch(helix.NodeVar("file")))
		q.ForEachParam("calls", callWriteBatch(helix.NodeVar("file")))
		q.ForEachParam("routes", routeWriteBatch(helix.NodeVar("file")))
		q.VarAs("file_id", helix.G().N(helix.NodeVar("file")).Project(helix.ProjectPropAs("$id", "id")))
		return q.Returning("file_id")
	}, &out)
	if err != nil {
		return 0, err
	}
	return firstID(out.FileID.Properties), nil
}

func (s *helixStore) GetFile(ctx context.Context, path string) (*api.FileInfo, error) {
	var out struct {
		Files helixRows[helixFileRow] `json:"files"`
	}
	q := helix.ReadQuery("code_context_get_file")
	keyParam := q.ParamString("key", helixKey(s.projectID, path))
	req := q.VarAs("files", fileTraversal().Where(helix.PredEq("key", keyParam)).Limit(1).Project(fileProjections()...)).Returning("files")
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}
	if len(out.Files.Properties) == 0 {
		return nil, nil
	}
	return out.Files.Properties[0].FileInfo(), nil
}

func (s *helixStore) DeleteFile(ctx context.Context, path string) error {
	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_delete_file")
		keyParam := q.ParamString("key", helixKey(s.projectID, path))
		projectParam := q.ParamString("project_id", s.projectID)
		pathParam := q.ParamString("path", path)
		q.VarAs("file", helix.G().NWithLabel(helixFileLabel).Where(helix.PredEq("key", keyParam)))
		q.VarAs("drop_symbols", helix.G().N(helix.NodeVar("file")).Out(helixDefinesEdge).Drop().Count())
		q.VarAs("drop_imports", helix.G().N(helix.NodeVar("file")).Out(helixImportsEdge).Drop().Count())
		q.VarAs("drop_calls", helix.G().N(helix.NodeVar("file")).Out(helixRecordsCallEdge).Drop().Count())
		q.VarAs("drop_routes", helix.G().N(helix.NodeVar("file")).Out(helixDeclaresRouteEdge).Drop().Count())
		q.VarAs("drop_embedding_chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).
			Where(helix.PredEq("project_id", projectParam)).
			Where(helix.PredEq("target_path", pathParam)).
			Drop().Count())
		q.VarAs("drop_file", helix.G().N(helix.NodeVar("file")).Drop().Count())
		return q.Returning()
	}, nil)
}

func (s *helixStore) ListFiles(ctx context.Context, lang *api.Language) ([]*api.FileInfo, error) {
	var out struct {
		Files helixRows[helixFileRow] `json:"files"`
	}
	q := helix.ReadQuery("code_context_list_files")
	tr := fileTraversal().Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID)))
	if lang != nil {
		tr = tr.Where(helix.PredEq("language", q.ParamString("language", string(*lang))))
	}
	if err := s.execRead(ctx, q.VarAs("files", tr.Project(fileProjections()...)).Returning("files"), &out); err != nil {
		return nil, err
	}
	result := make([]*api.FileInfo, 0, len(out.Files.Properties))
	for _, row := range out.Files.Properties {
		result = append(result, row.FileInfo())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func (s *helixStore) ReplaceSymbols(ctx context.Context, fileID int64, symbols []api.Symbol) error {
	file, err := s.getFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if file == nil {
		return nil
	}
	rows := symbolParamRows(s.projectID, file.Path, symbols)
	return s.replaceChildNodes(ctx, fileID, "code_context_replace_symbols", "symbols", rows, helixDefinesEdge, symbolWriteBatch(helix.NodeVar("file")))
}

func (s *helixStore) ReplaceImports(ctx context.Context, fileID int64, imports []api.ImportEdge) error {
	file, err := s.getFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if file == nil {
		return nil
	}
	rows := importParamRows(s.projectID, file.Path, imports)
	return s.replaceChildNodes(ctx, fileID, "code_context_replace_imports", "imports", rows, helixImportsEdge, importWriteBatch(helix.NodeVar("file")))
}

func (s *helixStore) ReplaceCalls(ctx context.Context, fileID int64, calls []api.CallEdge) error {
	file, err := s.getFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if file == nil {
		return nil
	}
	rows := callParamRows(s.projectID, file.Path, calls)
	return s.replaceChildNodes(ctx, fileID, "code_context_replace_calls", "calls", rows, helixRecordsCallEdge, callWriteBatch(helix.NodeVar("file")))
}

func (s *helixStore) ReplaceRoutes(ctx context.Context, fileID int64, routes []api.Route) error {
	file, err := s.getFileByID(ctx, fileID)
	if err != nil {
		return err
	}
	if file == nil {
		return nil
	}
	rows := routeParamRows(s.projectID, file.Path, routes)
	return s.replaceChildNodes(ctx, fileID, "code_context_replace_routes", "routes", rows, helixDeclaresRouteEdge, routeWriteBatch(helix.NodeVar("file")))
}

func (s *helixStore) SearchSymbols(ctx context.Context, query string, kind *api.SymbolKind, limit int) ([]api.Symbol, error) {
	if limit <= 0 {
		limit = 50
	}
	q := helix.ReadQuery("code_context_search_symbols")
	limitParam := q.ParamI64("limit", int64(limit))
	projectParam := q.ParamString("project_id", s.projectID)
	searchText := strings.TrimSpace(query)
	var tr *helix.Traversal
	if searchText == "" {
		tr = symbolTraversal().Limit(limitParam)
	} else {
		tr = helix.G().TextSearchNodesWith(helixSymbolLabel, "search_text", q.ParamString("query", searchText).Input(), limitParam.Bound(), nil)
	}
	tr = tr.Where(helix.PredEq("project_id", projectParam))
	if kind != nil {
		tr = tr.Where(helix.PredEq("kind", q.ParamString("kind", string(*kind))))
	}
	var out struct {
		Symbols helixRows[api.Symbol] `json:"symbols"`
	}
	if err := s.execRead(ctx, q.VarAs("symbols", tr.Project(symbolProjections()...)).Returning("symbols"), &out); err != nil {
		return nil, err
	}
	symbols := out.Symbols.Properties
	sortSymbols(symbols)
	if len(symbols) > limit {
		symbols = symbols[:limit]
	}
	return symbols, nil
}

func (s *helixStore) SearchText(ctx context.Context, query TextSearchQuery) ([]SearchHit, error) {
	if strings.TrimSpace(query.Query) == "" {
		return nil, nil
	}
	if !searchFilterAllowsProject(s.projectID, query.Filter) {
		return nil, nil
	}

	req, includeSymbols, includeDocuments, limit := helixTextSearchRequest(s.projectID, query)
	if !includeSymbols && !includeDocuments {
		return nil, nil
	}

	var out struct {
		Symbols   helixRows[helixTextSymbolRow]   `json:"symbols"`
		Documents helixRows[helixTextDocumentRow] `json:"documents"`
	}
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}

	hits := helixTextRowsToHits(s.projectID, out.Symbols.Properties, out.Documents.Properties, query.Filter)
	if query.Offset > 0 {
		if query.Offset >= len(hits) {
			return nil, nil
		}
		hits = hits[query.Offset:]
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *helixStore) GetEmbedding(ctx context.Context, key string) (*EmbeddingCacheEntry, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	var out struct {
		Chunks helixRows[helixEmbeddingChunkRow] `json:"chunks"`
	}
	q := helix.ReadQuery("code_context_get_embedding")
	physicalKey := helixEmbeddingKey(s.projectID, key)
	req := q.VarAs("chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("key", q.ParamString("key", physicalKey))).
		Limit(1).
		Project(helixEmbeddingChunkProjections()...)).
		Returning("chunks")
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}
	if len(out.Chunks.Properties) == 0 {
		return nil, nil
	}
	return out.Chunks.Properties[0].Entry()
}

func (s *helixStore) UpsertEmbedding(ctx context.Context, entry EmbeddingCacheEntry) error {
	entry.Key = strings.TrimSpace(entry.Key)
	entry.Model = strings.TrimSpace(entry.Model)
	if entry.Key == "" {
		return fmt.Errorf("embedding cache key is required")
	}
	if entry.Model == "" {
		return fmt.Errorf("embedding model is required")
	}
	if len(entry.Values) == 0 {
		return fmt.Errorf("embedding vector is required")
	}
	if entry.Dimensions <= 0 {
		entry.Dimensions = len(entry.Values)
	}
	if entry.ContentHash == "" {
		return fmt.Errorf("embedding content hash is required")
	}

	target := normalizeGraphStart(entry.Target)
	target.ProjectID = s.projectID
	targetKey := helixEmbeddingTargetKey(target)
	vectorProperty := helixEmbeddingVectorProperty(entry.Model, entry.Dimensions)
	physicalKey := helixEmbeddingKey(s.projectID, entry.Key)
	targetJSON, err := json.Marshal(target)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(nonNilStringMap(entry.Metadata))
	if err != nil {
		return err
	}
	vectorJSON, err := json.Marshal(entry.Values)
	if err != nil {
		return err
	}
	now := time.Now().Unix()

	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_upsert_embedding")
		projectID := q.ParamString("project_id", s.projectID)
		key := q.ParamString("key", physicalKey)
		cacheKey := q.ParamString("cache_key", entry.Key)
		model := q.ParamString("model", entry.Model)
		dimensions := q.ParamI64("dimensions", int64(entry.Dimensions))
		contentHash := q.ParamString("content_hash", entry.ContentHash)
		inputKind := q.ParamString("input_kind", string(entry.InputKind))
		targetKind := q.ParamString("target_kind", string(target.Kind))
		targetPath := q.ParamString("target_path", target.Path)
		targetName := q.ParamString("target_name", target.Name)
		targetType := q.ParamString("target_type", target.Type)
		targetLine := q.ParamI64("target_line", int64(target.Line))
		targetEndLine := q.ParamI64("target_end_line", int64(target.EndLine))
		targetMethod := q.ParamString("target_method", target.Method)
		targetRoutePath := q.ParamString("target_route_path", target.RoutePath)
		targetValue := q.ParamString("target_value", target.Value)
		targetKeyParam := q.ParamString("target_key", targetKey)
		targetJSONParam := q.ParamString("target_json", string(targetJSON))
		metadataJSONParam := q.ParamString("metadata_json", string(metadataJSON))
		vectorJSONParam := q.ParamString("vector_json", string(vectorJSON))
		vectorPropertyParam := q.ParamString("embedding_property", vectorProperty)
		vector := q.ParamArray("embedding", entry.Values, helix.ParamTypeF32())
		createdAt := q.ParamI64("created_at", now)
		updatedAt := q.ParamI64("updated_at", now)

		q.VarAs("vector_index", helix.G().CreateIndexIfNotExists(helix.NodeVectorIndex(helixEmbeddingChunkLabel, vectorProperty, "project_id")))
		q.VarAs("existing", helix.G().NWithLabel(helixEmbeddingChunkLabel).Where(helix.PredEq("key", key)))
		update := helix.G().N(helix.NodeVar("existing")).
			SetProperty("project_id", projectID).
			SetProperty("cache_key", cacheKey).
			SetProperty("model", model).
			SetProperty("dimensions", dimensions).
			SetProperty("content_hash", contentHash).
			SetProperty("input_kind", inputKind).
			SetProperty("target_kind", targetKind).
			SetProperty("target_path", targetPath).
			SetProperty("target_name", targetName).
			SetProperty("target_type", targetType).
			SetProperty("target_line", targetLine).
			SetProperty("target_end_line", targetEndLine).
			SetProperty("target_method", targetMethod).
			SetProperty("target_route_path", targetRoutePath).
			SetProperty("target_value", targetValue).
			SetProperty("target_key", targetKeyParam).
			SetProperty("target_json", targetJSONParam).
			SetProperty("metadata_json", metadataJSONParam).
			SetProperty("vector_json", vectorJSONParam).
			SetProperty("embedding_property", vectorPropertyParam).
			SetProperty(vectorProperty, vector).
			SetProperty("updated_at", updatedAt).
			Project(helix.ProjectPropAs("$id", "id"))
		q.VarAsIf("updated", helix.VarNotEmpty("existing"), update)
		q.VarAsIf("created", helix.VarEmpty("existing"), helix.G().AddN(helixEmbeddingChunkLabel, helix.Props{
			helix.Prop("key", key),
			helix.Prop("cache_key", cacheKey),
			helix.Prop("project_id", projectID),
			helix.Prop("model", model),
			helix.Prop("dimensions", dimensions),
			helix.Prop("content_hash", contentHash),
			helix.Prop("input_kind", inputKind),
			helix.Prop("target_kind", targetKind),
			helix.Prop("target_path", targetPath),
			helix.Prop("target_name", targetName),
			helix.Prop("target_type", targetType),
			helix.Prop("target_line", targetLine),
			helix.Prop("target_end_line", targetEndLine),
			helix.Prop("target_method", targetMethod),
			helix.Prop("target_route_path", targetRoutePath),
			helix.Prop("target_value", targetValue),
			helix.Prop("target_key", targetKeyParam),
			helix.Prop("target_json", targetJSONParam),
			helix.Prop("metadata_json", metadataJSONParam),
			helix.Prop("vector_json", vectorJSONParam),
			helix.Prop("embedding_property", vectorPropertyParam),
			helix.Prop(vectorProperty, vector),
			helix.Prop("created_at", createdAt),
			helix.Prop("updated_at", updatedAt),
		}).Project(helix.ProjectPropAs("$id", "id")))
		return q.Returning("updated", "created")
	}, nil)
}

func (s *helixStore) ListEmbeddingNamespaces(ctx context.Context) ([]EmbeddingNamespace, error) {
	var out struct {
		Chunks helixRows[helixEmbeddingChunkRow] `json:"chunks"`
	}
	q := helix.ReadQuery("code_context_list_embedding_namespaces")
	req := q.VarAs("chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Project(helixEmbeddingChunkProjections()...)).
		Returning("chunks")
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}

	acc := newEmbeddingNamespaceAccumulator()
	for _, row := range out.Chunks.Properties {
		acc.Add(row.Model, row.Dimensions, EmbeddingInputKind(row.InputKind), TargetKind(row.TargetKind), 1, timeFromUnixSeconds(row.CreatedAt), timeFromUnixSeconds(row.UpdatedAt))
	}
	return acc.List(), nil
}

func (s *helixStore) DeleteEmbeddingNamespace(ctx context.Context, model string, dimensions int) (int, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return 0, fmt.Errorf("embedding model is required")
	}
	if dimensions <= 0 {
		return 0, fmt.Errorf("embedding dimensions are required")
	}
	var out struct {
		Deleted helixCount `json:"deleted"`
	}
	err := s.execWrite(ctx, func() helix.Request {
		return helixDeleteEmbeddingNamespaceRequest(s.projectID, model, dimensions)
	}, &out)
	if err != nil {
		return 0, err
	}
	return out.Deleted.Count, nil
}

func (s *helixStore) SearchVector(ctx context.Context, query VectorSearchQuery) ([]SearchHit, error) {
	if len(query.Vector) == 0 {
		return nil, nil
	}
	if !searchFilterAllowsProject(s.projectID, query.Filter) {
		return nil, nil
	}
	model := vectorSearchModel(query)
	if model == "" {
		return nil, fmt.Errorf("vector search embedding model is required")
	}
	dimensions := query.Dimensions
	if dimensions <= 0 {
		dimensions = len(query.Vector)
	}
	if dimensions != len(query.Vector) {
		return nil, fmt.Errorf("vector dimensions = %d, query vector length = %d", dimensions, len(query.Vector))
	}

	req, limit := helixVectorSearchRequest(s.projectID, query, model, dimensions)
	var out struct {
		Chunks helixRows[helixVectorChunkRow] `json:"chunks"`
	}
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}
	hits := helixVectorRowsToHits(s.projectID, out.Chunks.Properties, query.Filter)
	if query.Offset > 0 {
		if query.Offset >= len(hits) {
			return nil, nil
		}
		hits = hits[query.Offset:]
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func helixTextSearchRequest(projectID string, query TextSearchQuery) (helix.Request, bool, bool, int) {
	includeSymbols, includeDocuments := helixTextSearchTargets(query.Filter.TargetKinds)
	limit := helixTextSearchLimit(query.Limit)

	q := helix.ReadQuery("code_context_search_text")
	queryParam := q.ParamString("query", strings.TrimSpace(query.Query))
	limitParam := q.ParamI64("limit", int64(limit))
	projectParam := q.ParamString("project_id", projectID)

	returning := make([]string, 0, 2)
	if includeSymbols {
		q.VarAs("symbols", helix.G().
			TextSearchNodesWith(helixSymbolLabel, "search_text", queryParam.Input(), limitParam.Bound(), nil).
			Where(helix.PredEq("project_id", projectParam)).
			Project(helixTextSymbolProjections()...))
		returning = append(returning, "symbols")
	}
	if includeDocuments {
		q.VarAs("documents", helix.G().
			TextSearchNodesWith(helixDocumentLabel, "search_text", queryParam.Input(), limitParam.Bound(), nil).
			Where(helix.PredEq("project_id", projectParam)).
			Project(helixTextDocumentProjections()...))
		returning = append(returning, "documents")
	}

	return q.Returning(returning...), includeSymbols, includeDocuments, limit
}

func helixTextSearchTargets(kinds []TargetKind) (includeSymbols bool, includeDocuments bool) {
	if len(kinds) == 0 {
		return true, true
	}
	for _, kind := range kinds {
		switch kind {
		case TargetFile, TargetSymbol, TargetText:
			includeSymbols = true
		case TargetDocument:
			includeDocuments = true
		}
	}
	return includeSymbols, includeDocuments
}

func helixTextSearchLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func helixDeleteEmbeddingNamespaceRequest(projectID, model string, dimensions int) helix.Request {
	q := helix.WriteQuery("code_context_delete_embedding_namespace")
	projectParam := q.ParamString("project_id", projectID)
	modelParam := q.ParamString("model", model)
	dimensionsParam := q.ParamI64("dimensions", int64(dimensions))
	return q.VarAs("deleted", helix.G().NWithLabel(helixEmbeddingChunkLabel).
		Where(helix.PredEq("project_id", projectParam)).
		Where(helix.PredEq("model", modelParam)).
		Where(helix.PredEq("dimensions", dimensionsParam)).
		Drop().
		Count()).
		Returning("deleted")
}

func helixVectorSearchRequest(projectID string, query VectorSearchQuery, model string, dimensions int) (helix.Request, int) {
	limit := helixTextSearchLimit(query.Limit)
	searchLimit := limit
	if len(query.Filter.TargetKinds) > 0 || query.Filter.FilePattern != "" || len(query.Filter.Metadata) > 0 {
		searchLimit = limit * 4
		if searchLimit < 20 {
			searchLimit = 20
		}
	}
	vectorProperty := helixEmbeddingVectorProperty(model, dimensions)

	q := helix.ReadQuery("code_context_search_vector")
	vector := q.ParamArray("query_vector", query.Vector, helix.ParamTypeF32())
	limitParam := q.ParamI64("limit", int64(searchLimit))
	projectParam := q.ParamString("project_id", projectID)
	projectInput := projectParam.Input()
	modelParam := q.ParamString("model", model)
	dimensionsParam := q.ParamI64("dimensions", int64(dimensions))

	tr := helix.G().
		VectorSearchNodesWith(helixEmbeddingChunkLabel, vectorProperty, vector.Input(), limitParam.Bound(), &projectInput).
		Where(helix.PredEq("project_id", projectParam)).
		Where(helix.PredEq("model", modelParam)).
		Where(helix.PredEq("dimensions", dimensionsParam)).
		Project(helixVectorChunkProjections()...)
	return q.VarAs("chunks", tr).Returning("chunks"), limit
}

func helixVectorRowsToHits(projectID string, rows []helixVectorChunkRow, filter SearchFilter) []SearchHit {
	hits := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		target, err := row.Target()
		if err != nil {
			continue
		}
		target.ProjectID = projectID
		if !vectorFilterAllows(projectID, filter, target, row) {
			continue
		}
		metadata, _ := parseStringMapJSON(row.MetadataJSON)
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["backend"] = "helix"
		metadata["model"] = row.Model
		metadata["dimensions"] = strconv.Itoa(row.Dimensions)
		metadata["embedding_property"] = row.EmbeddingProperty
		evidence := firstNonEmpty(metadata["signature"], metadata["title"], target.Name, target.Path, row.ContentHash)
		hits = append(hits, SearchHit{
			Target:   target,
			Score:    helixTextScore(row.Score),
			Source:   SearchSourceVector,
			Evidence: evidence,
			Highlights: []SearchHighlight{{
				Line:    target.Line,
				Snippet: evidence,
			}},
			Metadata: metadata,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Target.Path != hits[j].Target.Path {
			return hits[i].Target.Path < hits[j].Target.Path
		}
		if hits[i].Target.Line != hits[j].Target.Line {
			return hits[i].Target.Line < hits[j].Target.Line
		}
		return hits[i].Target.Name < hits[j].Target.Name
	})
	return hits
}

func vectorSearchModel(query VectorSearchQuery) string {
	if strings.TrimSpace(query.Model) != "" {
		return strings.TrimSpace(query.Model)
	}
	for _, key := range []string{"embedding_model", "model"} {
		if value := strings.TrimSpace(query.Filter.Metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func vectorFilterAllows(projectID string, filter SearchFilter, target TargetRef, row helixVectorChunkRow) bool {
	if !graphTraversalFilterAllows(projectID, filter, target) {
		return false
	}
	for key, value := range filter.Metadata {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "embedding_model", "model":
			if row.Model != value {
				return false
			}
		case "embedding_dimensions", "dimensions":
			if strconv.Itoa(row.Dimensions) != value {
				return false
			}
		}
	}
	return true
}

func searchFilterAllowsProject(projectID string, filter SearchFilter) bool {
	if len(filter.ProjectIDs) == 0 {
		return true
	}
	for _, id := range filter.ProjectIDs {
		if id == projectID {
			return true
		}
	}
	return false
}

func (s *helixStore) TraverseGraph(ctx context.Context, query GraphTraversalQuery) (*GraphTraversalResult, error) {
	start := normalizeGraphStart(query.Start)
	if start.Kind == "" && strings.TrimSpace(query.Target) != "" {
		start = ParseTargetRef(query.Target)
	}
	if start.Kind == "" {
		return &GraphTraversalResult{}, nil
	}
	if start.ProjectID == "" {
		start.ProjectID = s.projectID
	}

	maxDepth := query.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	if maxDepth > 3 {
		maxDepth = 3
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	direction, err := normalizeGraphDirection(query.Direction)
	if err != nil {
		return nil, err
	}
	edgeKinds, allowed, err := normalizeGraphEdgeKinds(query.EdgeKinds)
	if err != nil {
		return nil, err
	}
	if !searchFilterAllowsProject(s.projectID, query.Filter) {
		return &GraphTraversalResult{
			Start:     start,
			Direction: direction,
			MaxDepth:  maxDepth,
			EdgeKinds: edgeKinds,
			Summary:   fmt.Sprintf("graph traversal skipped because project %q is excluded by the filter", s.projectID),
		}, nil
	}

	opts := graphTraversalOptions{filter: query.Filter, fanoutLimit: limit}
	builder := newGraphTraversalBuilder(s.projectID)
	builder.addNode(start, nil, 0, true)
	queue := []graphFrontier{{target: start, depth: 0}}
	seenDepth := map[string]int{graphTargetKey(start): 0}
	parents := map[string]graphPathHop{}

	for len(queue) > 0 && builder.edgeCount() < limit {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= maxDepth {
			continue
		}

		var adjacent []graphTraversalNext
		if direction == GraphOutbound || direction == GraphBoth {
			next, err := s.expandGraphOutbound(ctx, builder, current.target, allowed, current.depth+1, opts)
			if err != nil {
				return nil, err
			}
			adjacent = append(adjacent, next...)
		}
		if direction == GraphInbound || direction == GraphBoth {
			next, err := s.expandGraphInbound(ctx, builder, current.target, allowed, current.depth+1, opts)
			if err != nil {
				return nil, err
			}
			adjacent = append(adjacent, next...)
		}

		for _, next := range adjacent {
			target := normalizeGraphStart(next.target)
			if target.ProjectID == "" {
				target.ProjectID = s.projectID
			}
			key := graphTargetKey(target)
			nextDepth := current.depth + 1
			if prev, ok := seenDepth[key]; ok && prev <= nextDepth {
				continue
			}
			seenDepth[key] = nextDepth
			parents[key] = graphPathHop{from: current.target, to: target, edgeKind: next.edgeKind, direction: next.direction}
			queue = append(queue, graphFrontier{target: target, depth: nextDepth})
		}
	}

	result := builder.result()
	if len(result.Edges) > limit {
		result.Edges = result.Edges[:limit]
	}
	if len(result.Nodes) > limit+1 {
		result.Nodes = result.Nodes[:limit+1]
	}
	result.Start = start
	result.Direction = direction
	result.MaxDepth = maxDepth
	result.EdgeKinds = edgeKinds
	if query.IncludePaths {
		result.Paths = graphTraversalPaths(start, result.Nodes, parents)
	}
	result.Summary = summarizeGraphTraversal(result)
	return result, nil
}

type graphFrontier struct {
	target TargetRef
	depth  int
}

type graphTraversalOptions struct {
	filter      SearchFilter
	fanoutLimit int
}

type graphTraversalNext struct {
	target    TargetRef
	edgeKind  GraphEdgeKind
	direction GraphDirection
}

type graphPathHop struct {
	from      TargetRef
	to        TargetRef
	edgeKind  GraphEdgeKind
	direction GraphDirection
}

func (s *helixStore) expandGraphOutbound(ctx context.Context, builder *graphTraversalBuilder, from TargetRef, allowed map[GraphEdgeKind]struct{}, depth int, opts graphTraversalOptions) ([]graphTraversalNext, error) {
	var adjacent []graphTraversalNext
	switch from.Kind {
	case TargetFile:
		if graphEdgeAllowed(allowed, GraphEdgeDefines) && from.Path != "" {
			symbols, err := s.GetFileSymbols(ctx, from.Path)
			if err != nil {
				return nil, err
			}
			for _, sym := range symbols {
				to := graphSymbolTarget(sym)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeDefines, graphProperties("line", fmt.Sprint(sym.Line), "kind", string(sym.Kind)), depth, GraphOutbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeImports) && from.Path != "" {
			imports, err := s.GetImports(ctx, from.Path)
			if err != nil {
				return nil, err
			}
			for _, imp := range imports {
				to := TargetRef{Kind: TargetText, Type: "import", Value: imp.ToSource}
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeImports, graphProperties("line", fmt.Sprint(imp.Line), "source", imp.ToSource), depth, GraphOutbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeRoutes) && from.Path != "" {
			routes, err := s.ListRoutes(ctx, "")
			if err != nil {
				return nil, err
			}
			for _, route := range routes {
				if route.FilePath != from.Path {
					continue
				}
				to := graphRouteTarget(route)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeRoutes, graphProperties("line", fmt.Sprint(route.Line), "framework", route.Framework), depth, GraphOutbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeDocuments) && from.Path != "" {
			docs, err := s.GetDocumentsByTarget(ctx, "file", from.Path)
			if err != nil {
				return nil, err
			}
			for _, link := range docs {
				to := graphDocumentTarget(link.DocumentPath)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeDocuments, graphDocumentLinkProperties(link), depth, GraphOutbound, opts)
			}
		}
	case TargetSymbol:
		name := graphSymbolName(from)
		if graphEdgeAllowed(allowed, GraphEdgeCalls) && name != "" {
			calls, err := s.GetCallees(ctx, name)
			if err != nil {
				return nil, err
			}
			for _, call := range calls {
				to := TargetRef{Kind: TargetSymbol, Name: call.ToName}
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeCalls, graphProperties("line", fmt.Sprint(call.Line), "from_file", call.FromFile, "confidence", call.Confidence), depth, GraphOutbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeRoutes) && name != "" {
			routes, err := s.ListRoutes(ctx, name)
			if err != nil {
				return nil, err
			}
			for _, route := range routes {
				if route.Handler != name && !strings.HasSuffix(route.Handler, "."+name) && !strings.HasSuffix(route.Handler, "::"+name) {
					continue
				}
				to := graphRouteTarget(route)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeRoutes, graphProperties("line", fmt.Sprint(route.Line), "framework", route.Framework, "handler", route.Handler), depth, GraphOutbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeDocuments) && name != "" {
			docs, err := s.GetDocumentsByTarget(ctx, "symbol", name)
			if err != nil {
				return nil, err
			}
			for _, link := range docs {
				to := graphDocumentTarget(link.DocumentPath)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeDocuments, graphDocumentLinkProperties(link), depth, GraphOutbound, opts)
			}
		}
	case TargetDocument:
		if graphEdgeAllowed(allowed, GraphEdgeReferences) && from.Path != "" {
			links, err := s.GetDocumentLinks(ctx, from.Path)
			if err != nil {
				return nil, err
			}
			for _, link := range links {
				to := graphDocumentLinkTarget(link)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeReferences, graphDocumentLinkProperties(link), depth, GraphOutbound, opts)
			}
		}
	case TargetRoute:
		if graphEdgeAllowed(allowed, GraphEdgeDocuments) {
			targetValue := graphRouteValue(from)
			if targetValue == "" {
				break
			}
			docs, err := s.GetDocumentsByTarget(ctx, "route", targetValue)
			if err != nil {
				return nil, err
			}
			for _, link := range docs {
				to := graphDocumentTarget(link.DocumentPath)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeDocuments, graphDocumentLinkProperties(link), depth, GraphOutbound, opts)
			}
		}
	case TargetText:
		if graphEdgeAllowed(allowed, GraphEdgeSimilar) && from.Value != "" {
			limit := opts.fanoutLimit
			if limit <= 0 || limit > 25 {
				limit = 25
			}
			hits, err := s.SearchText(ctx, TextSearchQuery{Query: from.Value, Filter: opts.filter, Limit: limit})
			if err != nil {
				return nil, err
			}
			for _, hit := range hits {
				to := hit.Target
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeSimilar, graphProperties("source", string(hit.Source), "score", fmt.Sprintf("%.4f", hit.Score), "evidence", hit.Evidence), depth, GraphOutbound, opts)
			}
		}
	}
	return adjacent, nil
}

func (s *helixStore) expandGraphInbound(ctx context.Context, builder *graphTraversalBuilder, to TargetRef, allowed map[GraphEdgeKind]struct{}, depth int, opts graphTraversalOptions) ([]graphTraversalNext, error) {
	var adjacent []graphTraversalNext
	switch to.Kind {
	case TargetFile:
		if graphEdgeAllowed(allowed, GraphEdgeReferences) && to.Path != "" {
			docs, err := s.GetDocumentsByTarget(ctx, "file", to.Path)
			if err != nil {
				return nil, err
			}
			for _, link := range docs {
				from := graphDocumentTarget(link.DocumentPath)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeReferences, graphDocumentLinkProperties(link), depth, GraphInbound, opts)
			}
		}
	case TargetSymbol:
		name := graphSymbolName(to)
		if graphEdgeAllowed(allowed, GraphEdgeCalls) && name != "" {
			calls, err := s.GetCallers(ctx, name)
			if err != nil {
				return nil, err
			}
			for _, call := range calls {
				from := TargetRef{Kind: TargetSymbol, Path: call.FromFile, Name: call.FromSymbol, Line: call.Line}
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeCalls, graphProperties("line", fmt.Sprint(call.Line), "from_file", call.FromFile, "confidence", call.Confidence), depth, GraphInbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeReferences) && name != "" {
			docs, err := s.GetDocumentsByTarget(ctx, "symbol", name)
			if err != nil {
				return nil, err
			}
			for _, link := range docs {
				from := graphDocumentTarget(link.DocumentPath)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeReferences, graphDocumentLinkProperties(link), depth, GraphInbound, opts)
			}
		}
	case TargetRoute:
		if graphEdgeAllowed(allowed, GraphEdgeRoutes) {
			routes, err := s.ListRoutes(ctx, graphRouteQuery(to))
			if err != nil {
				return nil, err
			}
			for _, route := range routes {
				if !graphRouteMatches(to, route) || route.Handler == "" {
					continue
				}
				from := TargetRef{Kind: TargetSymbol, Path: route.FilePath, Name: route.Handler, Line: route.Line}
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeRoutes, graphProperties("line", fmt.Sprint(route.Line), "framework", route.Framework, "handler", route.Handler), depth, GraphInbound, opts)
			}
		}
		if graphEdgeAllowed(allowed, GraphEdgeReferences) {
			targetValue := graphRouteValue(to)
			if targetValue == "" {
				break
			}
			docs, err := s.GetDocumentsByTarget(ctx, "route", targetValue)
			if err != nil {
				return nil, err
			}
			for _, link := range docs {
				from := graphDocumentTarget(link.DocumentPath)
				adjacent = s.addGraphTraversalEdge(builder, adjacent, from, to, GraphEdgeReferences, graphDocumentLinkProperties(link), depth, GraphInbound, opts)
			}
		}
	}
	return adjacent, nil
}

func (s *helixStore) addGraphTraversalEdge(builder *graphTraversalBuilder, adjacent []graphTraversalNext, from, to TargetRef, kind GraphEdgeKind, properties map[string]string, depth int, direction GraphDirection, opts graphTraversalOptions) []graphTraversalNext {
	from = normalizeGraphStart(from)
	to = normalizeGraphStart(to)
	if from.ProjectID == "" {
		from.ProjectID = s.projectID
	}
	if to.ProjectID == "" {
		to.ProjectID = s.projectID
	}
	traversalTarget := to
	fromDepth, toDepth := depth-1, depth
	if direction == GraphInbound {
		traversalTarget = from
		fromDepth, toDepth = depth, depth-1
	}
	if !graphTraversalFilterAllows(s.projectID, opts.filter, traversalTarget) {
		return adjacent
	}
	builder.addEdge(from, to, kind, properties, fromDepth, toDepth, depth)
	return append(adjacent, graphTraversalNext{target: traversalTarget, edgeKind: kind, direction: direction})
}

func (s *helixStore) FindDefinitions(ctx context.Context, name string) ([]api.Symbol, error) {
	q := helix.ReadQuery("code_context_find_definitions")
	nameParam := q.ParamString("name", name)
	tr := symbolTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("name", nameParam)).
		Where(definitionKindPredicate()).
		Project(symbolProjections()...)
	var out struct {
		Symbols helixRows[api.Symbol] `json:"symbols"`
	}
	if err := s.execRead(ctx, q.VarAs("symbols", tr).Returning("symbols"), &out); err != nil {
		return nil, err
	}
	symbols := out.Symbols.Properties
	sortSymbols(symbols)
	return symbols, nil
}

func (s *helixStore) FindReferences(ctx context.Context, name string) ([]api.Symbol, error) {
	q := helix.ReadQuery("code_context_find_references")
	nameParam := q.ParamString("name", name)
	var out struct {
		Symbols helixRows[api.Symbol] `json:"symbols"`
	}
	tr := symbolTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("name", nameParam)).
		Project(symbolProjections()...)
	if err := s.execRead(ctx, q.VarAs("symbols", tr).Returning("symbols"), &out); err != nil {
		return nil, err
	}
	symbols := out.Symbols.Properties
	sortSymbols(symbols)
	return symbols, nil
}

func (s *helixStore) GetFileSymbols(ctx context.Context, path string) ([]api.Symbol, error) {
	q := helix.ReadQuery("code_context_get_file_symbols")
	pathParam := q.ParamString("file_path", path)
	var out struct {
		Symbols helixRows[api.Symbol] `json:"symbols"`
	}
	tr := symbolTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("file_path", pathParam)).
		OrderBy("line", helix.OrderAsc).
		Project(symbolProjections()...)
	if err := s.execRead(ctx, q.VarAs("symbols", tr).Returning("symbols"), &out); err != nil {
		return nil, err
	}
	return out.Symbols.Properties, nil
}

func (s *helixStore) GetImports(ctx context.Context, filePath string) ([]api.ImportEdge, error) {
	q := helix.ReadQuery("code_context_get_imports")
	fileParam := q.ParamString("file_path", filePath)
	var out struct {
		Imports helixRows[api.ImportEdge] `json:"imports"`
	}
	tr := importTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("file_path", fileParam)).
		OrderBy("line", helix.OrderAsc).
		Project(importProjections()...)
	if err := s.execRead(ctx, q.VarAs("imports", tr).Returning("imports"), &out); err != nil {
		return nil, err
	}
	return out.Imports.Properties, nil
}

func (s *helixStore) GetImporters(ctx context.Context, importSource string) ([]api.ImportEdge, error) {
	q := helix.ReadQuery("code_context_get_importers")
	sourceParam := q.ParamString("source", importSource)
	var out struct {
		Imports helixRows[api.ImportEdge] `json:"imports"`
	}
	tr := importTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredContainsExpr("source", sourceParam.Expr())).
		Project(importProjections()...)
	if err := s.execRead(ctx, q.VarAs("imports", tr).Returning("imports"), &out); err != nil {
		return nil, err
	}
	return out.Imports.Properties, nil
}

func (s *helixStore) GetCallees(ctx context.Context, fromSymbol string) ([]api.CallEdge, error) {
	q := helix.ReadQuery("code_context_get_callees")
	fromParam := q.ParamString("from_symbol", fromSymbol)
	var out struct {
		Calls helixRows[api.CallEdge] `json:"calls"`
	}
	tr := callTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("from_symbol", fromParam)).
		OrderBy("line", helix.OrderAsc).
		Project(callProjections()...)
	if err := s.execRead(ctx, q.VarAs("calls", tr).Returning("calls"), &out); err != nil {
		return nil, err
	}
	return out.Calls.Properties, nil
}

func (s *helixStore) GetCallers(ctx context.Context, toName string) ([]api.CallEdge, error) {
	q := helix.ReadQuery("code_context_get_callers")
	toParam := q.ParamString("to_name", toName)
	var out struct {
		Calls helixRows[api.CallEdge] `json:"calls"`
	}
	tr := callTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredContainsExpr("to_name", toParam.Expr())).
		Project(callProjections()...)
	if err := s.execRead(ctx, q.VarAs("calls", tr).Returning("calls"), &out); err != nil {
		return nil, err
	}
	calls := out.Calls.Properties
	filtered := calls[:0]
	for _, call := range calls {
		if call.ToName == toName || strings.HasSuffix(call.ToName, "."+toName) || strings.HasSuffix(call.ToName, "::"+toName) {
			filtered = append(filtered, call)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].FromFile != filtered[j].FromFile {
			return filtered[i].FromFile < filtered[j].FromFile
		}
		return filtered[i].Line < filtered[j].Line
	})
	return filtered, nil
}

func (s *helixStore) ListRoutes(ctx context.Context, query string) ([]api.Route, error) {
	q := helix.ReadQuery("code_context_list_routes")
	query = strings.TrimSpace(query)
	tr := routeTraversal().Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID)))
	if query != "" {
		queryParam := q.ParamString("query", query)
		tr = tr.Where(helix.PredOr(
			helix.PredContainsExpr("path", queryParam.Expr()),
			helix.PredContainsExpr("handler", queryParam.Expr()),
			helix.PredContainsExpr("framework", queryParam.Expr()),
			helix.PredContainsExpr("file_path", queryParam.Expr()),
		))
	}
	var out struct {
		Routes helixRows[api.Route] `json:"routes"`
	}
	if err := s.execRead(ctx, q.VarAs("routes", tr.Project(routeProjections()...)).Returning("routes"), &out); err != nil {
		return nil, err
	}
	routes := out.Routes.Properties
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes, nil
}

func (s *helixStore) Stats(ctx context.Context) (*api.IndexStats, error) {
	var out struct {
		Files     helixCount `json:"files"`
		Symbols   helixCount `json:"symbols"`
		Imports   helixCount `json:"imports"`
		Documents helixCount `json:"documents"`
	}
	q := helix.ReadQuery("code_context_stats")
	projectParam := q.ParamString("project_id", s.projectID)
	req := q.
		VarAs("files", helix.G().NWithLabel(helixFileLabel).Where(helix.PredEq("project_id", projectParam)).Count()).
		VarAs("symbols", helix.G().NWithLabel(helixSymbolLabel).Where(helix.PredEq("project_id", projectParam)).Count()).
		VarAs("imports", helix.G().NWithLabel(helixImportLabel).Where(helix.PredEq("project_id", projectParam)).Count()).
		VarAs("documents", helix.G().NWithLabel(helixDocumentLabel).Where(helix.PredEq("project_id", projectParam)).Count()).
		Returning("files", "symbols", "imports", "documents")
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}
	files, err := s.ListFiles(ctx, nil)
	if err != nil {
		return nil, err
	}
	var lastIndexed int64
	for _, f := range files {
		if f.IndexedAt > lastIndexed {
			lastIndexed = f.IndexedAt
		}
	}
	stats := &api.IndexStats{TotalFiles: out.Files.Count, TotalSymbols: out.Symbols.Count, TotalImports: out.Imports.Count, TotalDocuments: out.Documents.Count, LastIndexedUnix: lastIndexed, IndexVersion: "graph-export.v2"}
	if lastIndexed > 0 {
		stats.LastIndexedAt = time.Unix(lastIndexed, 0).UTC().Format(time.RFC3339)
	}
	return stats, nil
}

func (s *helixStore) SchemaStatus(ctx context.Context) (*api.SchemaStatus, error) {
	return &api.SchemaStatus{
		ExpectedVersion: HelixSchemaVersion,
		AppliedVersion:  HelixSchemaVersion,
		VersionOK:       true,
		Tables:          []string{helixFileLabel, helixSymbolLabel, helixImportLabel, helixCallLabel, helixRouteLabel, helixDocumentLabel, helixDocumentLinkLabel, helixEmbeddingChunkLabel},
		Indexes:         []string{"file.key", "file.project_id", "symbol.key", "symbol.project_id", "symbol.search_text", "document.key", "document.project_id", "document.search_text", "document_link.target_key", "embedding_chunk.key", "embedding_chunk.cache_key", "embedding_chunk.project_id", "embedding_chunk.model", "embedding_chunk.target_key", "embedding_chunk.target_path", "embedding_chunk.<namespace>"},
	}, nil
}

func (s *helixStore) ResetIndex(ctx context.Context) error {
	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_reset_index")
		projectParam := q.ParamString("project_id", s.projectID)
		q.VarAs("embedding_chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("document_links", helix.G().NWithLabel(helixDocumentLinkLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("documents", helix.G().NWithLabel(helixDocumentLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("routes", helix.G().NWithLabel(helixRouteLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("calls", helix.G().NWithLabel(helixCallLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("imports", helix.G().NWithLabel(helixImportLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("symbols", helix.G().NWithLabel(helixSymbolLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		q.VarAs("files", helix.G().NWithLabel(helixFileLabel).Where(helix.PredEq("project_id", projectParam)).Drop().Count())
		return q.Returning()
	}, nil)
}

func (s *helixStore) UpsertDocument(ctx context.Context, doc *api.Document) (int64, error) {
	if doc == nil {
		return 0, fmt.Errorf("document is required")
	}
	now := time.Now().Unix()
	var out struct {
		Updated helixRows[idRow] `json:"updated"`
		Created helixRows[idRow] `json:"created"`
	}
	err := s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_upsert_document")
		projectID := q.ParamString("project_id", s.projectID)
		key := q.ParamString("key", helixKey(s.projectID, doc.Path))
		path := q.ParamString("path", doc.Path)
		language := q.ParamString("language", doc.Language)
		contentHash := q.ParamString("content_hash", doc.ContentHash)
		title := q.ParamString("title", doc.Title)
		summary := q.ParamString("summary", doc.Summary)
		searchText := q.ParamString("search_text", documentSearchText(doc))
		size := q.ParamI64("size", int64(doc.Size))
		indexedAt := q.ParamI64("indexed_at", now)
		q.VarAs("existing", helix.G().NWithLabel(helixDocumentLabel).Where(helix.PredEq("key", key)))
		q.VarAs("drop_embedding_chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).
			Where(helix.PredEq("project_id", projectID)).
			Where(helix.PredEq("target_path", path)).
			Drop().Count())
		q.VarAsIf("updated", helix.VarNotEmpty("existing"), helix.G().N(helix.NodeVar("existing")).
			SetProperty("project_id", projectID).
			SetProperty("language", language).
			SetProperty("content_hash", contentHash).
			SetProperty("title", title).
			SetProperty("summary", summary).
			SetProperty("search_text", searchText).
			SetProperty("size", size).
			SetProperty("indexed_at", indexedAt).
			Project(helix.ProjectPropAs("$id", "id")))
		q.VarAsIf("created", helix.VarEmpty("existing"), helix.G().AddN(helixDocumentLabel, helix.Props{
			helix.Prop("key", key),
			helix.Prop("project_id", projectID),
			helix.Prop("path", path),
			helix.Prop("language", language),
			helix.Prop("content_hash", contentHash),
			helix.Prop("title", title),
			helix.Prop("summary", summary),
			helix.Prop("search_text", searchText),
			helix.Prop("size", size),
			helix.Prop("indexed_at", indexedAt),
		}).Project(helix.ProjectPropAs("$id", "id")))
		return q.Returning("updated", "created")
	}, &out)
	if err != nil {
		return 0, err
	}
	return firstID(out.Updated.Properties, out.Created.Properties), nil
}

func (s *helixStore) GetDocument(ctx context.Context, path string) (*api.Document, error) {
	q := helix.ReadQuery("code_context_get_document")
	keyParam := q.ParamString("key", helixKey(s.projectID, path))
	var out struct {
		Documents helixRows[api.Document] `json:"documents"`
	}
	if err := s.execRead(ctx, q.VarAs("documents", documentTraversal().Where(helix.PredEq("key", keyParam)).Limit(1).Project(documentProjections()...)).Returning("documents"), &out); err != nil {
		return nil, err
	}
	if len(out.Documents.Properties) == 0 {
		return nil, nil
	}
	return &out.Documents.Properties[0], nil
}

func (s *helixStore) DeleteDocument(ctx context.Context, path string) error {
	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_delete_document")
		keyParam := q.ParamString("key", helixKey(s.projectID, path))
		projectParam := q.ParamString("project_id", s.projectID)
		pathParam := q.ParamString("path", path)
		q.VarAs("document", helix.G().NWithLabel(helixDocumentLabel).Where(helix.PredEq("key", keyParam)))
		q.VarAs("drop_links", helix.G().N(helix.NodeVar("document")).Out(helixDocumentLinkEdge).Drop().Count())
		q.VarAs("drop_embedding_chunks", helix.G().NWithLabel(helixEmbeddingChunkLabel).
			Where(helix.PredEq("project_id", projectParam)).
			Where(helix.PredEq("target_path", pathParam)).
			Drop().Count())
		q.VarAs("drop_document", helix.G().N(helix.NodeVar("document")).Drop().Count())
		return q.Returning()
	}, nil)
}

func (s *helixStore) ListDocuments(ctx context.Context) ([]*api.Document, error) {
	q := helix.ReadQuery("code_context_list_documents")
	var out struct {
		Documents helixRows[api.Document] `json:"documents"`
	}
	tr := documentTraversal().Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).Project(documentProjections()...)
	if err := s.execRead(ctx, q.VarAs("documents", tr).Returning("documents"), &out); err != nil {
		return nil, err
	}
	result := make([]*api.Document, 0, len(out.Documents.Properties))
	for i := range out.Documents.Properties {
		result = append(result, &out.Documents.Properties[i])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func (s *helixStore) ReplaceDocumentLinks(ctx context.Context, docID int64, links []api.DocumentLink) error {
	doc, err := s.getDocumentByID(ctx, docID)
	if err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	rows := documentLinkParamRows(s.projectID, doc.Path, docID, links)
	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery("code_context_replace_document_links")
		q.ParamArray("links", rows, helix.ParamTypeObject())
		q.VarAs("document", helix.G().N(helix.NodeID(uint64(docID))).Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))))
		q.VarAs("drop_links", helix.G().N(helix.NodeVar("document")).Out(helixDocumentLinkEdge).Drop().Count())
		q.ForEachParam("links", documentLinkWriteBatch(helix.NodeVar("document")))
		return q.Returning()
	}, nil)
}

func (s *helixStore) GetDocumentLinks(ctx context.Context, docPath string) ([]api.DocumentLink, error) {
	q := helix.ReadQuery("code_context_get_document_links")
	pathParam := q.ParamString("document_path", docPath)
	var out struct {
		Links helixRows[api.DocumentLink] `json:"links"`
	}
	tr := documentLinkTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("document_path", pathParam)).
		OrderBy("line", helix.OrderAsc).
		Project(documentLinkProjections()...)
	if err := s.execRead(ctx, q.VarAs("links", tr).Returning("links"), &out); err != nil {
		return nil, err
	}
	return out.Links.Properties, nil
}

func (s *helixStore) GetDocumentsByTarget(ctx context.Context, targetType, targetValue string) ([]api.DocumentLink, error) {
	q := helix.ReadQuery("code_context_get_documents_by_target")
	targetKey := q.ParamString("target_key", targetType+":"+targetValue)
	var out struct {
		Links helixRows[api.DocumentLink] `json:"links"`
	}
	tr := documentLinkTraversal().
		Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).
		Where(helix.PredEq("target_key", targetKey)).
		Project(documentLinkProjections()...)
	if err := s.execRead(ctx, q.VarAs("links", tr).Returning("links"), &out); err != nil {
		return nil, err
	}
	return out.Links.Properties, nil
}

func (s *helixStore) GetDocumentStats(ctx context.Context) (total, indexed int, err error) {
	var out struct {
		Documents helixCount `json:"documents"`
	}
	q := helix.ReadQuery("code_context_document_stats")
	req := q.VarAs("documents", helix.G().NWithLabel(helixDocumentLabel).Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).Count()).Returning("documents")
	if err := s.execRead(ctx, req, &out); err != nil {
		return 0, 0, err
	}
	return out.Documents.Count, out.Documents.Count, nil
}

func (s *helixStore) Close() error { return nil }

func (s *helixStore) replaceChildNodes(ctx context.Context, fileID int64, queryName, param string, rows []map[string]any, edge string, body *helix.WriteBatch) error {
	return s.execWrite(ctx, func() helix.Request {
		q := helix.WriteQuery(queryName)
		q.ParamArray(param, rows, helix.ParamTypeObject())
		q.VarAs("file", helix.G().N(helix.NodeID(uint64(fileID))).Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))))
		q.VarAs("drop_existing", helix.G().N(helix.NodeVar("file")).Out(edge).Drop().Count())
		q.ForEachParam(param, body)
		return q.Returning()
	}, nil)
}

func (s *helixStore) getFileByID(ctx context.Context, id int64) (*api.FileInfo, error) {
	var out struct {
		Files helixRows[helixFileRow] `json:"files"`
	}
	q := helix.ReadQuery("code_context_get_file_by_id")
	req := q.
		VarAs("files", helix.G().N(helix.NodeID(uint64(id))).Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).Project(fileProjections()...)).
		Returning("files")
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}
	if len(out.Files.Properties) == 0 {
		return nil, nil
	}
	return out.Files.Properties[0].FileInfo(), nil
}

func (s *helixStore) getDocumentByID(ctx context.Context, id int64) (*api.Document, error) {
	var out struct {
		Documents helixRows[api.Document] `json:"documents"`
	}
	q := helix.ReadQuery("code_context_get_document_by_id")
	req := q.
		VarAs("documents", helix.G().N(helix.NodeID(uint64(id))).Where(helix.PredEq("project_id", q.ParamString("project_id", s.projectID))).Project(documentProjections()...)).
		Returning("documents")
	if err := s.execRead(ctx, req, &out); err != nil {
		return nil, err
	}
	if len(out.Documents.Properties) == 0 {
		return nil, nil
	}
	return &out.Documents.Properties[0], nil
}

func (s *helixStore) execWrite(ctx context.Context, build func() helix.Request, out any) error {
	return helixExecWithRetry(ctx, s.writeRetryAttempts, s.writeRetryBackoff, func() error {
		return s.client.Exec(ctx, build(), out, helix.WriterOnly(), helix.AwaitDurability(true))
	}, helixShouldRetryWrite)
}

func (s *helixStore) execRead(ctx context.Context, req helix.Request, out any) error {
	return helixExecWithRetry(ctx, s.readRetryAttempts, s.readRetryBackoff, func() error {
		return s.client.Exec(ctx, req, out)
	}, helixShouldRetryRead)
}

func helixExecWithRetry(ctx context.Context, attempts int, backoff time.Duration, exec func() error, shouldRetry func(error) bool) error {
	if attempts <= 0 {
		attempts = 1
	}
	if backoff < 0 {
		backoff = 0
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		err = exec()
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if attempt == attempts-1 || shouldRetry == nil || !shouldRetry(err) {
			return err
		}
		if backoff == 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * backoff):
		}
	}
	return err
}

func helixShouldRetryRead(err error) bool {
	return helixIsTransient(err)
}

func helixShouldRetryWrite(err error) bool {
	return helix.IsConflict(err) || helixIsTransient(err)
}

func helixIsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var helixErr *helix.HelixError
	if !errors.As(err, &helixErr) {
		return false
	}
	switch helixErr.Kind {
	case helix.ErrorNetwork:
		return true
	case helix.ErrorRemote:
		switch helixErr.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

type idRow struct {
	ID int64 `json:"id"`
}

type helixRows[T any] struct {
	Properties []T `json:"properties"`
}

type helixCount struct {
	Count int `json:"count"`
}

func firstID(groups ...[]idRow) int64 {
	for _, group := range groups {
		if len(group) > 0 {
			return group[0].ID
		}
	}
	return 0
}

type helixFileRow struct {
	ID          int64  `json:"id"`
	Path        string `json:"path"`
	Language    string `json:"language"`
	ContentHash string `json:"content_hash"`
	Size        int64  `json:"size"`
	IndexedAt   int64  `json:"indexed_at"`
}

func (r helixFileRow) FileInfo() *api.FileInfo {
	return &api.FileInfo{Path: r.Path, Language: api.Language(r.Language), ContentHash: r.ContentHash, Size: r.Size, IndexedAt: r.IndexedAt}
}

type helixTextSymbolRow struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	FilePath   string  `json:"file"`
	Line       int     `json:"line"`
	EndLine    int     `json:"end_line"`
	Signature  string  `json:"signature"`
	Parent     string  `json:"parent"`
	SearchText string  `json:"search_text"`
	Score      float64 `json:"score"`
}

type helixTextDocumentRow struct {
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Summary    string  `json:"summary"`
	SearchText string  `json:"search_text"`
	Score      float64 `json:"score"`
}

type helixEmbeddingChunkRow struct {
	Key               string `json:"key"`
	CacheKey          string `json:"cache_key"`
	Model             string `json:"model"`
	Dimensions        int    `json:"dimensions"`
	ContentHash       string `json:"content_hash"`
	InputKind         string `json:"input_kind"`
	TargetKind        string `json:"target_kind"`
	TargetPath        string `json:"target_path"`
	TargetName        string `json:"target_name"`
	TargetType        string `json:"target_type"`
	TargetLine        int    `json:"target_line"`
	TargetEndLine     int    `json:"target_end_line"`
	TargetMethod      string `json:"target_method"`
	TargetRoutePath   string `json:"target_route_path"`
	TargetValue       string `json:"target_value"`
	TargetKey         string `json:"target_key"`
	TargetJSON        string `json:"target_json"`
	MetadataJSON      string `json:"metadata_json"`
	VectorJSON        string `json:"vector_json"`
	EmbeddingProperty string `json:"embedding_property"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

func (r helixEmbeddingChunkRow) Entry() (*EmbeddingCacheEntry, error) {
	target, err := r.Target()
	if err != nil {
		return nil, err
	}
	metadata, err := parseStringMapJSON(r.MetadataJSON)
	if err != nil {
		return nil, err
	}
	var values []float32
	if strings.TrimSpace(r.VectorJSON) != "" {
		if err := json.Unmarshal([]byte(r.VectorJSON), &values); err != nil {
			return nil, err
		}
	}
	return &EmbeddingCacheEntry{
		Key:         firstNonEmpty(r.CacheKey, r.Key),
		Model:       r.Model,
		Dimensions:  r.Dimensions,
		ContentHash: r.ContentHash,
		InputKind:   EmbeddingInputKind(r.InputKind),
		Target:      target,
		Values:      values,
		Metadata:    metadata,
		CreatedAt:   time.Unix(r.CreatedAt, 0),
		UpdatedAt:   time.Unix(r.UpdatedAt, 0),
	}, nil
}

func (r helixEmbeddingChunkRow) Target() (TargetRef, error) {
	var target TargetRef
	if strings.TrimSpace(r.TargetJSON) != "" {
		if err := json.Unmarshal([]byte(r.TargetJSON), &target); err != nil {
			return target, err
		}
	}
	if target.Kind == "" {
		target.Kind = TargetKind(r.TargetKind)
	}
	if target.Path == "" {
		target.Path = r.TargetPath
	}
	if target.Name == "" {
		target.Name = r.TargetName
	}
	if target.Type == "" {
		target.Type = r.TargetType
	}
	if target.Line == 0 {
		target.Line = r.TargetLine
	}
	if target.EndLine == 0 {
		target.EndLine = r.TargetEndLine
	}
	if target.Method == "" {
		target.Method = r.TargetMethod
	}
	if target.RoutePath == "" {
		target.RoutePath = r.TargetRoutePath
	}
	if target.Value == "" {
		target.Value = r.TargetValue
	}
	return normalizeGraphStart(target), nil
}

type helixVectorChunkRow struct {
	helixEmbeddingChunkRow
	Score float64 `json:"score"`
}

func fileTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixFileLabel)
}

func fileProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("$id", "id"),
		helix.ProjectPropAs("path", "path"),
		helix.ProjectPropAs("language", "language"),
		helix.ProjectPropAs("content_hash", "content_hash"),
		helix.ProjectPropAs("size", "size"),
		helix.ProjectPropAs("indexed_at", "indexed_at"),
	}
}

func symbolTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixSymbolLabel)
}

func symbolProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("name", "name"),
		helix.ProjectPropAs("kind", "kind"),
		helix.ProjectPropAs("file_path", "file"),
		helix.ProjectPropAs("line", "line"),
		helix.ProjectPropAs("end_line", "end_line"),
		helix.ProjectPropAs("signature", "signature"),
		helix.ProjectPropAs("parent", "parent"),
	}
}

func helixTextSymbolProjections() []helix.Projection {
	projections := append([]helix.Projection{}, symbolProjections()...)
	projections = append(projections,
		helix.ProjectPropAs("search_text", "search_text"),
		helix.ProjectPropAs("$distance", "score"),
	)
	return projections
}

func importTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixImportLabel)
}

func importProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("file_path", "from"),
		helix.ProjectPropAs("source", "to"),
		helix.ProjectPropAs("line", "line"),
	}
}

func callTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixCallLabel)
}

func callProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("file_path", "from_file"),
		helix.ProjectPropAs("from_symbol", "from_symbol"),
		helix.ProjectPropAs("to_name", "to_name"),
		helix.ProjectPropAs("line", "line"),
		helix.ProjectPropAs("confidence", "confidence"),
	}
}

func routeTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixRouteLabel)
}

func routeProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("file_path", "file"),
		helix.ProjectPropAs("method", "method"),
		helix.ProjectPropAs("path", "path"),
		helix.ProjectPropAs("handler", "handler"),
		helix.ProjectPropAs("framework", "framework"),
		helix.ProjectPropAs("line", "line"),
		helix.ProjectPropAs("confidence", "confidence"),
	}
}

func documentTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixDocumentLabel)
}

func documentProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("$id", "id"),
		helix.ProjectPropAs("path", "path"),
		helix.ProjectPropAs("language", "language"),
		helix.ProjectPropAs("content_hash", "content_hash"),
		helix.ProjectPropAs("title", "title"),
		helix.ProjectPropAs("summary", "summary"),
		helix.ProjectPropAs("size", "size"),
		helix.ProjectPropAs("indexed_at", "indexed_at"),
	}
}

func helixTextDocumentProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("path", "path"),
		helix.ProjectPropAs("title", "title"),
		helix.ProjectPropAs("summary", "summary"),
		helix.ProjectPropAs("search_text", "search_text"),
		helix.ProjectPropAs("$distance", "score"),
	}
}

func helixEmbeddingChunkProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("key", "key"),
		helix.ProjectPropAs("cache_key", "cache_key"),
		helix.ProjectPropAs("model", "model"),
		helix.ProjectPropAs("dimensions", "dimensions"),
		helix.ProjectPropAs("content_hash", "content_hash"),
		helix.ProjectPropAs("input_kind", "input_kind"),
		helix.ProjectPropAs("target_kind", "target_kind"),
		helix.ProjectPropAs("target_path", "target_path"),
		helix.ProjectPropAs("target_name", "target_name"),
		helix.ProjectPropAs("target_type", "target_type"),
		helix.ProjectPropAs("target_line", "target_line"),
		helix.ProjectPropAs("target_end_line", "target_end_line"),
		helix.ProjectPropAs("target_method", "target_method"),
		helix.ProjectPropAs("target_route_path", "target_route_path"),
		helix.ProjectPropAs("target_value", "target_value"),
		helix.ProjectPropAs("target_key", "target_key"),
		helix.ProjectPropAs("target_json", "target_json"),
		helix.ProjectPropAs("metadata_json", "metadata_json"),
		helix.ProjectPropAs("vector_json", "vector_json"),
		helix.ProjectPropAs("embedding_property", "embedding_property"),
		helix.ProjectPropAs("created_at", "created_at"),
		helix.ProjectPropAs("updated_at", "updated_at"),
	}
}

func helixVectorChunkProjections() []helix.Projection {
	projections := append([]helix.Projection{}, helixEmbeddingChunkProjections()...)
	projections = append(projections, helix.ProjectPropAs("$distance", "score"))
	return projections
}

func documentLinkTraversal() *helix.Traversal {
	return helix.G().NWithLabel(helixDocumentLinkLabel)
}

func documentLinkProjections() []helix.Projection {
	return []helix.Projection{
		helix.ProjectPropAs("$id", "id"),
		helix.ProjectPropAs("document_id", "document_id"),
		helix.ProjectPropAs("document_path", "document_path"),
		helix.ProjectPropAs("target_type", "target_type"),
		helix.ProjectPropAs("target_value", "target_value"),
		helix.ProjectPropAs("line", "line"),
		helix.ProjectPropAs("section_title", "section_title"),
		helix.ProjectPropAs("section_slug", "section_slug"),
		helix.ProjectPropAs("section_line", "section_line"),
		helix.ProjectPropAs("evidence", "evidence"),
		helix.ProjectPropAs("confidence", "confidence"),
	}
}

func symbolWriteBatch(file helix.NodeRef) *helix.WriteBatch {
	return helix.Write().
		VarAs("symbol", helix.G().AddN(helixSymbolLabel, helix.Props{
			helix.Prop("key", helix.ExprParam("key")),
			helix.Prop("project_id", helix.ExprParam("project_id")),
			helix.Prop("file_path", helix.ExprParam("file_path")),
			helix.Prop("name", helix.ExprParam("name")),
			helix.Prop("kind", helix.ExprParam("kind")),
			helix.Prop("line", helix.ExprParam("line")),
			helix.Prop("end_line", helix.ExprParam("end_line")),
			helix.Prop("signature", helix.ExprParam("signature")),
			helix.Prop("parent", helix.ExprParam("parent")),
			helix.Prop("search_text", helix.ExprParam("search_text")),
		})).
		VarAs("defines", helix.G().N(file).AddE(helixDefinesEdge, helix.NodeVar("symbol"), helix.Props{
			helix.Prop("line", helix.ExprParam("line")),
		}))
}

func importWriteBatch(file helix.NodeRef) *helix.WriteBatch {
	return helix.Write().
		VarAs("import", helix.G().AddN(helixImportLabel, helix.Props{
			helix.Prop("key", helix.ExprParam("key")),
			helix.Prop("project_id", helix.ExprParam("project_id")),
			helix.Prop("file_path", helix.ExprParam("file_path")),
			helix.Prop("source", helix.ExprParam("source")),
			helix.Prop("line", helix.ExprParam("line")),
		})).
		VarAs("imports", helix.G().N(file).AddE(helixImportsEdge, helix.NodeVar("import"), helix.Props{
			helix.Prop("line", helix.ExprParam("line")),
		}))
}

func callWriteBatch(file helix.NodeRef) *helix.WriteBatch {
	return helix.Write().
		VarAs("call", helix.G().AddN(helixCallLabel, helix.Props{
			helix.Prop("key", helix.ExprParam("key")),
			helix.Prop("project_id", helix.ExprParam("project_id")),
			helix.Prop("file_path", helix.ExprParam("file_path")),
			helix.Prop("from_symbol", helix.ExprParam("from_symbol")),
			helix.Prop("to_name", helix.ExprParam("to_name")),
			helix.Prop("line", helix.ExprParam("line")),
			helix.Prop("confidence", helix.ExprParam("confidence")),
		})).
		VarAs("records_call", helix.G().N(file).AddE(helixRecordsCallEdge, helix.NodeVar("call"), helix.Props{
			helix.Prop("line", helix.ExprParam("line")),
		}))
}

func routeWriteBatch(file helix.NodeRef) *helix.WriteBatch {
	return helix.Write().
		VarAs("route", helix.G().AddN(helixRouteLabel, helix.Props{
			helix.Prop("key", helix.ExprParam("key")),
			helix.Prop("project_id", helix.ExprParam("project_id")),
			helix.Prop("file_path", helix.ExprParam("file_path")),
			helix.Prop("method", helix.ExprParam("method")),
			helix.Prop("path", helix.ExprParam("path")),
			helix.Prop("handler", helix.ExprParam("handler")),
			helix.Prop("framework", helix.ExprParam("framework")),
			helix.Prop("line", helix.ExprParam("line")),
			helix.Prop("confidence", helix.ExprParam("confidence")),
		})).
		VarAs("declares_route", helix.G().N(file).AddE(helixDeclaresRouteEdge, helix.NodeVar("route"), helix.Props{
			helix.Prop("line", helix.ExprParam("line")),
		}))
}

func documentLinkWriteBatch(document helix.NodeRef) *helix.WriteBatch {
	return helix.Write().
		VarAs("link", helix.G().AddN(helixDocumentLinkLabel, helix.Props{
			helix.Prop("key", helix.ExprParam("key")),
			helix.Prop("project_id", helix.ExprParam("project_id")),
			helix.Prop("document_id", helix.ExprParam("document_id")),
			helix.Prop("document_path", helix.ExprParam("document_path")),
			helix.Prop("target_key", helix.ExprParam("target_key")),
			helix.Prop("target_type", helix.ExprParam("target_type")),
			helix.Prop("target_value", helix.ExprParam("target_value")),
			helix.Prop("line", helix.ExprParam("line")),
			helix.Prop("section_title", helix.ExprParam("section_title")),
			helix.Prop("section_slug", helix.ExprParam("section_slug")),
			helix.Prop("section_line", helix.ExprParam("section_line")),
			helix.Prop("evidence", helix.ExprParam("evidence")),
			helix.Prop("confidence", helix.ExprParam("confidence")),
		})).
		VarAs("document_link", helix.G().N(document).AddE(helixDocumentLinkEdge, helix.NodeVar("link"), helix.Props{
			helix.Prop("line", helix.ExprParam("line")),
		}))
}

func helixTextRowsToHits(projectID string, symbols []helixTextSymbolRow, documents []helixTextDocumentRow, filter SearchFilter) []SearchHit {
	hits := make([]SearchHit, 0, len(symbols)+len(documents))
	for _, row := range symbols {
		if !matchesSearchFilePattern(row.FilePath, filter.FilePattern) {
			continue
		}
		evidence := firstNonEmpty(row.Signature, row.SearchText, row.Name)
		hits = append(hits, SearchHit{
			Target: TargetRef{
				ProjectID: projectID,
				Kind:      TargetSymbol,
				Path:      row.FilePath,
				Name:      row.Name,
				Type:      row.Kind,
				Line:      row.Line,
				EndLine:   row.EndLine,
			},
			Score:    helixTextScore(row.Score),
			Source:   SearchSourceText,
			Evidence: evidence,
			Highlights: []SearchHighlight{{
				Line:    row.Line,
				Snippet: evidence,
			}},
			Metadata: map[string]string{
				"backend": "helix",
				"kind":    row.Kind,
			},
		})
	}
	for _, row := range documents {
		if !matchesSearchFilePattern(row.Path, filter.FilePattern) {
			continue
		}
		evidence := firstNonEmpty(row.Summary, row.Title, row.SearchText, row.Path)
		hits = append(hits, SearchHit{
			Target: TargetRef{
				ProjectID: projectID,
				Kind:      TargetDocument,
				Path:      row.Path,
				Name:      row.Title,
				Type:      "document",
			},
			Score:    helixTextScore(row.Score),
			Source:   SearchSourceText,
			Evidence: evidence,
			Highlights: []SearchHighlight{{
				Snippet: evidence,
			}},
			Metadata: map[string]string{
				"backend": "helix",
				"kind":    "document",
			},
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Target.Path != hits[j].Target.Path {
			return hits[i].Target.Path < hits[j].Target.Path
		}
		if hits[i].Target.Line != hits[j].Target.Line {
			return hits[i].Target.Line < hits[j].Target.Line
		}
		return hits[i].Target.Name < hits[j].Target.Name
	})
	return hits
}

func helixTextScore(distance float64) float64 {
	if distance < 0 {
		return 0
	}
	return 1 / (1 + distance)
}

func matchesSearchFilePattern(filePath, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	if strings.Contains(filePath, pattern) {
		return true
	}
	if ok, _ := pathpkg.Match(pattern, filePath); ok {
		return true
	}
	if ok, _ := pathpkg.Match(pattern, pathpkg.Base(filePath)); ok {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func documentSearchText(doc *api.Document) string {
	if doc == nil {
		return ""
	}
	return strings.Join([]string{doc.Title, doc.Summary, doc.Path, doc.Language}, " ")
}

type graphTraversalBuilder struct {
	projectID string
	nodes     map[string]GraphNode
	nodeOrder []string
	edges     map[string]GraphEdge
	edgeOrder []string
}

func newGraphTraversalBuilder(projectID string) *graphTraversalBuilder {
	return &graphTraversalBuilder{
		projectID: projectID,
		nodes:     map[string]GraphNode{},
		edges:     map[string]GraphEdge{},
	}
}

func (b *graphTraversalBuilder) addNode(target TargetRef, properties map[string]string, depth int, root bool) {
	target = normalizeGraphStart(target)
	if target.ProjectID == "" {
		target.ProjectID = b.projectID
	}
	key := graphTargetKey(target)
	if key == "" {
		return
	}
	if existing, ok := b.nodes[key]; ok {
		if root {
			existing.Root = true
		}
		if depth > 0 && (existing.Depth == 0 || depth < existing.Depth) {
			existing.Depth = depth
		}
		if len(properties) > 0 {
			if existing.Properties == nil {
				existing.Properties = map[string]string{}
			}
			for k, v := range properties {
				if v != "" {
					existing.Properties[k] = v
				}
			}
			b.nodes[key] = existing
		}
		b.nodes[key] = existing
		return
	}
	b.nodes[key] = GraphNode{Target: target, Depth: depth, Root: root, Properties: properties}
	b.nodeOrder = append(b.nodeOrder, key)
}

func (b *graphTraversalBuilder) addEdge(from, to TargetRef, kind GraphEdgeKind, properties map[string]string, fromDepth, toDepth, edgeDepth int) {
	from = normalizeGraphStart(from)
	to = normalizeGraphStart(to)
	if from.ProjectID == "" {
		from.ProjectID = b.projectID
	}
	if to.ProjectID == "" {
		to.ProjectID = b.projectID
	}
	fromKey := graphTargetKey(from)
	toKey := graphTargetKey(to)
	if fromKey == "" || toKey == "" || kind == "" {
		return
	}
	b.addNode(from, nil, fromDepth, false)
	b.addNode(to, nil, toDepth, false)
	key := fromKey + "->" + toKey + "#" + string(kind)
	if _, ok := b.edges[key]; ok {
		return
	}
	b.edges[key] = GraphEdge{From: from, To: to, Kind: kind, Depth: edgeDepth, Properties: properties}
	b.edgeOrder = append(b.edgeOrder, key)
}

func (b *graphTraversalBuilder) edgeCount() int {
	return len(b.edges)
}

func (b *graphTraversalBuilder) result() *GraphTraversalResult {
	nodes := make([]GraphNode, 0, len(b.nodeOrder))
	for _, key := range b.nodeOrder {
		nodes = append(nodes, b.nodes[key])
	}
	edges := make([]GraphEdge, 0, len(b.edgeOrder))
	for _, key := range b.edgeOrder {
		edges = append(edges, b.edges[key])
	}
	return &GraphTraversalResult{Nodes: nodes, Edges: edges}
}

func normalizeGraphEdgeKinds(kinds []GraphEdgeKind) ([]GraphEdgeKind, map[GraphEdgeKind]struct{}, error) {
	if len(kinds) == 0 {
		return nil, nil, nil
	}
	allowed := make(map[GraphEdgeKind]struct{}, len(kinds))
	ordered := make([]GraphEdgeKind, 0, len(kinds))
	for _, kind := range kinds {
		kind = GraphEdgeKind(strings.TrimSpace(strings.ToLower(string(kind))))
		if kind == "" {
			continue
		}
		var expanded []GraphEdgeKind
		switch kind {
		case "all":
			return nil, nil, nil
		case "code":
			expanded = []GraphEdgeKind{GraphEdgeDefines, GraphEdgeImports, GraphEdgeCalls, GraphEdgeRoutes}
		case "docs":
			expanded = []GraphEdgeKind{GraphEdgeDocuments, GraphEdgeReferences}
		case "symbols":
			expanded = []GraphEdgeKind{GraphEdgeDefines, GraphEdgeCalls}
		case "entrypoints":
			expanded = []GraphEdgeKind{GraphEdgeRoutes}
		case GraphEdgeDefines, GraphEdgeImports, GraphEdgeCalls, GraphEdgeRoutes, GraphEdgeDocuments, GraphEdgeReferences, GraphEdgeSimilar:
			expanded = []GraphEdgeKind{kind}
		default:
			return nil, nil, fmt.Errorf("invalid graph edge kind %q", kind)
		}
		for _, edge := range expanded {
			if _, ok := allowed[edge]; ok {
				continue
			}
			allowed[edge] = struct{}{}
			ordered = append(ordered, edge)
		}
	}
	return ordered, allowed, nil
}

func graphEdgeKindSet(kinds []GraphEdgeKind) map[GraphEdgeKind]struct{} {
	_, allowed, _ := normalizeGraphEdgeKinds(kinds)
	return allowed
}

func graphEdgeAllowed(allowed map[GraphEdgeKind]struct{}, kind GraphEdgeKind) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[kind]
	return ok
}

func normalizeGraphDirection(direction GraphDirection) (GraphDirection, error) {
	if direction == "" {
		return GraphOutbound, nil
	}
	direction = GraphDirection(strings.TrimSpace(strings.ToLower(string(direction))))
	switch direction {
	case GraphOutbound, GraphInbound, GraphBoth:
		return direction, nil
	default:
		return "", fmt.Errorf("invalid graph direction %q", direction)
	}
}

func graphTraversalFilterAllows(projectID string, filter SearchFilter, target TargetRef) bool {
	if !searchFilterAllowsProject(projectID, filter) {
		return false
	}
	if len(filter.TargetKinds) > 0 {
		matched := false
		for _, kind := range filter.TargetKinds {
			if kind == target.Kind {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if strings.TrimSpace(filter.FilePattern) != "" {
		if target.Path == "" {
			return false
		}
		if !matchesSearchFilePattern(target.Path, filter.FilePattern) {
			return false
		}
	}
	for key, value := range filter.Metadata {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "kind", "target_kind":
			if string(target.Kind) != value {
				return false
			}
		case "type":
			if target.Type != value {
				return false
			}
		case "name":
			if target.Name != value {
				return false
			}
		case "path":
			if target.Path != value {
				return false
			}
		}
	}
	return true
}

func normalizeGraphStart(target TargetRef) TargetRef {
	if target.Kind == "" {
		switch {
		case target.Path != "":
			target.Kind = TargetFile
		case target.RoutePath != "" || target.Method != "":
			target.Kind = TargetRoute
		case target.Name != "":
			target.Kind = TargetSymbol
		case target.Value != "":
			target.Kind = TargetText
		}
	}
	if target.Kind == TargetRoute && target.Value == "" {
		target.Value = graphRouteValue(target)
	}
	if target.Kind == TargetDocument && target.Type == "" {
		target.Type = "document"
	}
	return target
}

func graphTraversalPaths(start TargetRef, nodes []GraphNode, parents map[string]graphPathHop) []GraphTraversalPath {
	start = normalizeGraphStart(start)
	paths := make([]GraphTraversalPath, 0, len(nodes))
	for _, node := range nodes {
		target := normalizeGraphStart(node.Target)
		if graphTargetKey(target) == graphTargetKey(start) {
			paths = append(paths, GraphTraversalPath{Target: target})
			continue
		}
		var reversed []GraphTraversalStep
		seen := map[string]struct{}{}
		current := target
		for {
			key := graphTargetKey(current)
			if key == "" {
				break
			}
			if _, ok := seen[key]; ok {
				break
			}
			seen[key] = struct{}{}
			hop, ok := parents[key]
			if !ok {
				break
			}
			reversed = append(reversed, GraphTraversalStep{
				From:      hop.from,
				To:        hop.to,
				EdgeKind:  hop.edgeKind,
				Direction: hop.direction,
			})
			if graphTargetKey(hop.from) == graphTargetKey(start) {
				break
			}
			current = hop.from
		}
		steps := make([]GraphTraversalStep, 0, len(reversed))
		for i := len(reversed) - 1; i >= 0; i-- {
			steps = append(steps, reversed[i])
		}
		if len(steps) == 0 {
			continue
		}
		paths = append(paths, GraphTraversalPath{Target: target, Depth: len(steps), Steps: steps})
	}
	return paths
}

func summarizeGraphTraversal(result *GraphTraversalResult) string {
	if result == nil {
		return ""
	}
	if len(result.Nodes) == 0 && len(result.Edges) == 0 {
		return fmt.Sprintf("Graph traversal from %s reached no related targets.", graphTargetLabel(result.Start))
	}
	counts := map[GraphEdgeKind]int{}
	for _, edge := range result.Edges {
		counts[edge.Kind]++
	}
	parts := make([]string, 0, len(counts))
	for kind, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, count))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return fmt.Sprintf("Graph traversal from %s reached %d node(s) and no edges.", graphTargetLabel(result.Start), len(result.Nodes))
	}
	return fmt.Sprintf("Graph traversal from %s reached %d node(s), %d edge(s) across %s.", graphTargetLabel(result.Start), len(result.Nodes), len(result.Edges), strings.Join(parts, ", "))
}

func graphTargetLabel(target TargetRef) string {
	target = normalizeGraphStart(target)
	switch target.Kind {
	case TargetFile, TargetDocument:
		return strings.TrimSpace(string(target.Kind) + ":" + target.Path)
	case TargetSymbol:
		if target.Path != "" {
			return "symbol:" + target.Path + "#" + graphSymbolName(target)
		}
		return "symbol:" + graphSymbolName(target)
	case TargetRoute:
		return "route:" + graphRouteValue(target)
	case TargetText:
		return "text:" + target.Value
	default:
		return strings.TrimSpace(string(target.Kind) + ":" + target.Value)
	}
}

func graphTargetKey(target TargetRef) string {
	target = normalizeGraphStart(target)
	switch target.Kind {
	case TargetFile:
		return "file:" + target.Path
	case TargetSymbol:
		if target.Path != "" {
			return "symbol:" + target.Path + ":" + target.Name
		}
		return "symbol:" + target.Name
	case TargetRoute:
		return "route:" + graphRouteValue(target)
	case TargetDocument:
		return "document:" + target.Path
	case TargetText:
		return "text:" + target.Type + ":" + target.Value
	default:
		return string(target.Kind) + ":" + target.Value
	}
}

func graphSymbolTarget(sym api.Symbol) TargetRef {
	return TargetRef{
		Kind:    TargetSymbol,
		Path:    sym.FilePath,
		Name:    sym.Name,
		Type:    string(sym.Kind),
		Line:    sym.Line,
		EndLine: sym.EndLine,
	}
}

func graphSymbolName(target TargetRef) string {
	if target.Name != "" {
		return target.Name
	}
	return target.Value
}

func graphRouteTarget(route api.Route) TargetRef {
	return TargetRef{
		Kind:      TargetRoute,
		Path:      route.FilePath,
		Name:      route.Handler,
		Type:      route.Framework,
		Line:      route.Line,
		Method:    route.Method,
		RoutePath: route.Path,
		Value:     strings.TrimSpace(route.Method + " " + route.Path),
	}
}

func graphRouteValue(target TargetRef) string {
	if target.Value != "" {
		return target.Value
	}
	if target.Method != "" && target.RoutePath != "" {
		return strings.TrimSpace(strings.ToUpper(target.Method) + " " + target.RoutePath)
	}
	if target.RoutePath != "" {
		return target.RoutePath
	}
	return strings.TrimSpace(target.Method)
}

func graphRouteQuery(target TargetRef) string {
	if target.RoutePath != "" {
		return target.RoutePath
	}
	if target.Value != "" {
		_, routePath := parseRouteTargetValue(target.Value)
		if routePath != "" {
			return routePath
		}
		return target.Value
	}
	return target.Name
}

func graphRouteMatches(target TargetRef, route api.Route) bool {
	if target.RoutePath != "" && route.Path != target.RoutePath {
		return false
	}
	if target.Method != "" && !strings.EqualFold(route.Method, target.Method) {
		return false
	}
	if target.Name != "" && route.Handler != target.Name && !strings.HasSuffix(route.Handler, "."+target.Name) && !strings.HasSuffix(route.Handler, "::"+target.Name) {
		return false
	}
	if target.Value != "" {
		method, routePath := parseRouteTargetValue(target.Value)
		if routePath != "" && route.Path != routePath {
			return false
		}
		if method != "" && !strings.EqualFold(route.Method, method) {
			return false
		}
	}
	return true
}

func graphDocumentTarget(path string) TargetRef {
	return TargetRef{Kind: TargetDocument, Path: path}
}

func graphDocumentLinkTarget(link api.DocumentLink) TargetRef {
	switch link.TargetType {
	case "file":
		return TargetRef{Kind: TargetFile, Path: link.TargetValue}
	case "symbol":
		return TargetRef{Kind: TargetSymbol, Name: link.TargetValue}
	case "route":
		method, routePath := parseRouteTargetValue(link.TargetValue)
		return TargetRef{Kind: TargetRoute, Method: method, RoutePath: routePath, Value: link.TargetValue}
	default:
		return TargetRef{Kind: TargetText, Type: link.TargetType, Value: link.TargetValue}
	}
}

func parseRouteTargetValue(value string) (method string, routePath string) {
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return "", parts[0]
	}
	return strings.ToUpper(parts[0]), strings.Join(parts[1:], " ")
}

func graphDocumentLinkProperties(link api.DocumentLink) map[string]string {
	return graphProperties(
		"document_path", link.DocumentPath,
		"target_type", link.TargetType,
		"target_value", link.TargetValue,
		"line", fmt.Sprint(link.Line),
		"section", link.SectionTitle,
		"evidence", link.Evidence,
		"confidence", fmt.Sprintf("%.2f", link.Confidence),
	)
}

func graphProperties(values ...string) map[string]string {
	props := map[string]string{}
	for i := 0; i+1 < len(values); i += 2 {
		key := values[i]
		value := strings.TrimSpace(values[i+1])
		if key != "" && value != "" {
			props[key] = value
		}
	}
	if len(props) == 0 {
		return nil
	}
	return props
}

func symbolParamRows(projectID, filePath string, symbols []api.Symbol) []map[string]any {
	rows := make([]map[string]any, 0, len(symbols))
	for _, sym := range symbols {
		if sym.FilePath == "" {
			sym.FilePath = filePath
		}
		rows = append(rows, map[string]any{
			"key":         symbolKeyForHelix(projectID, sym.FilePath, sym.Name, sym.Line),
			"project_id":  projectID,
			"file_path":   sym.FilePath,
			"name":        sym.Name,
			"kind":        string(sym.Kind),
			"line":        int64(sym.Line),
			"end_line":    int64(sym.EndLine),
			"signature":   sym.Signature,
			"parent":      sym.Parent,
			"search_text": strings.Join([]string{sym.Name, sym.Signature, sym.Parent, sym.FilePath, string(sym.Kind)}, " "),
		})
	}
	return rows
}

func importParamRows(projectID, filePath string, imports []api.ImportEdge) []map[string]any {
	rows := make([]map[string]any, 0, len(imports))
	for _, imp := range imports {
		if imp.FromFile == "" {
			imp.FromFile = filePath
		}
		rows = append(rows, map[string]any{
			"key":        helixKey(projectID, imp.FromFile, imp.ToSource, fmt.Sprint(imp.Line)),
			"project_id": projectID,
			"file_path":  imp.FromFile,
			"source":     imp.ToSource,
			"line":       int64(imp.Line),
		})
	}
	return rows
}

func callParamRows(projectID, filePath string, calls []api.CallEdge) []map[string]any {
	rows := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		if call.FromFile == "" {
			call.FromFile = filePath
		}
		confidence := call.Confidence
		if confidence == "" {
			confidence = "HEURISTIC"
		}
		rows = append(rows, map[string]any{
			"key":         helixKey(projectID, call.FromFile, call.FromSymbol, call.ToName, fmt.Sprint(call.Line)),
			"project_id":  projectID,
			"file_path":   call.FromFile,
			"from_symbol": call.FromSymbol,
			"to_name":     call.ToName,
			"line":        int64(call.Line),
			"confidence":  confidence,
		})
	}
	return rows
}

func routeParamRows(projectID, filePath string, routes []api.Route) []map[string]any {
	rows := make([]map[string]any, 0, len(routes))
	for _, route := range routes {
		if route.FilePath == "" {
			route.FilePath = filePath
		}
		confidence := route.Confidence
		if confidence == "" {
			confidence = "HEURISTIC"
		}
		rows = append(rows, map[string]any{
			"key":        helixKey(projectID, route.FilePath, route.Method, route.Path, route.Handler, fmt.Sprint(route.Line)),
			"project_id": projectID,
			"file_path":  route.FilePath,
			"method":     route.Method,
			"path":       route.Path,
			"handler":    route.Handler,
			"framework":  route.Framework,
			"line":       int64(route.Line),
			"confidence": confidence,
		})
	}
	return rows
}

func documentLinkParamRows(projectID, docPath string, docID int64, links []api.DocumentLink) []map[string]any {
	rows := make([]map[string]any, 0, len(links))
	for _, link := range links {
		documentPath := link.DocumentPath
		if documentPath == "" {
			documentPath = docPath
		}
		rows = append(rows, map[string]any{
			"key":           helixKey(projectID, documentPath, link.TargetType, link.TargetValue, fmt.Sprint(link.Line)),
			"project_id":    projectID,
			"document_id":   docID,
			"document_path": documentPath,
			"target_key":    link.TargetType + ":" + link.TargetValue,
			"target_type":   link.TargetType,
			"target_value":  link.TargetValue,
			"line":          int64(link.Line),
			"section_title": link.SectionTitle,
			"section_slug":  link.SectionSlug,
			"section_line":  int64(link.SectionLine),
			"evidence":      link.Evidence,
			"confidence":    link.Confidence,
		})
	}
	return rows
}

func symbolKeyForHelix(projectID, filePath, name string, line int) string {
	return helixKey(projectID, filePath, name, fmt.Sprint(line))
}

func helixKey(parts ...string) string {
	return strings.Join(parts, "\x1f")
}

func helixEmbeddingKey(projectID, cacheKey string) string {
	return helixKey(projectID, cacheKey)
}

func helixEmbeddingTargetKey(target TargetRef) string {
	target = normalizeGraphStart(target)
	return helixKey(
		string(target.Kind),
		target.Path,
		target.Name,
		target.Type,
		fmt.Sprint(target.Line),
		target.Method,
		target.RoutePath,
		target.Value,
	)
}

func helixEmbeddingVectorProperty(model string, dimensions int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(model),
		fmt.Sprint(dimensions),
	}, "\x00")))
	return "embedding_" + hex.EncodeToString(sum[:8])
}

func parseStringMapJSON(value string) (map[string]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func definitionKindPredicate() helix.Predicate {
	return helix.PredOr(
		helix.PredEq("kind", string(api.Function)),
		helix.PredEq("kind", string(api.Method)),
		helix.PredEq("kind", string(api.Class)),
		helix.PredEq("kind", string(api.Type)),
		helix.PredEq("kind", string(api.Interface)),
	)
}

func sortSymbols(symbols []api.Symbol) {
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].FilePath != symbols[j].FilePath {
			return symbols[i].FilePath < symbols[j].FilePath
		}
		if symbols[i].Line != symbols[j].Line {
			return symbols[i].Line < symbols[j].Line
		}
		return symbols[i].Name < symbols[j].Name
	})
}
