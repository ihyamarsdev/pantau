# Issue 01: Memori agent berlingkup, recall lokal, dan pemotongan hasil tool yang jujur

Status: ready-for-agent

## Deskripsi
Implementasikan **Agent Memory** berlingkup dan recall lokal sesuai [ADR 0082](../../../docs/adr/0082-scoped-agent-memory-and-local-recall.md) dan spesifikasi di `.scratch/agent-memory-recall/spec.md`: satu catatan membawa paling banyak satu lingkup (fleet, host, cluster, domain), hanya catatan dan fakta inventaris yang relevan dengan pertanyaan yang masuk ke prompt, dan hasil tool yang besar dipotong satu kali dengan pemberitahuan yang jujur.

## Kriteria Penerimaan

### Skema dan store
- [x] `agent_memory.cluster_id` (FK, CASCADE) dan `agent_memory.domain` (TEXT, default '') — perubahan additive, catatan lama tetap berlaku.
- [x] `AgentMemoryScope` dengan `Empty()`, `normalizeScope` (menolak dua lingkup: `ErrAgentMemoryScope`; domain tak dikenal: `ErrAgentMemoryDomain`), `scopeArgs`.
- [x] `AddAgentMemoryScoped`: teks yang sama pada lingkup yang sama hanya disegarkan; lingkup lain = catatan baru.
- [x] `RecallAgentMemory(scope, query, limit)`: skor relevansi, catatan host/cluster lain dibuang, limit dihormati.
- [x] `RecallContext`: satu pencarian Fleet Search per kata kunci; fakta menyebut jenis, nama, jumlah host, dan nama host.
- [x] Snapshot menyalin `cluster_id` dan `domain`.

### Prompt
- [x] `agentMemoryPrompt(question)` hanya menyuntik catatan yang cocok; sisanya dijangkau `memory_list`.
- [x] Tidak ada catatan yang cocok tetapi ada fakta → fakta tetap masuk.
- [x] `memoryScopeOf` mempersempit (host lebih sempit daripada domain), tidak pernah memperluas.

### Pemotongan hasil tool
- [x] `truncateToolResult`: kepala + ekor + pemberitahuan (jumlah byte, hint per tool); hint tak dikenal → kalimat umum.
- [x] `shrinkOldToolResults`: 4 terbaru utuh, yang lebih lama dipangkas ke 2 KiB dan diumumkan.
- [x] `callAgentTool` tidak memotong lagi; `clip` yang lama dihapus.

### Operator
- [x] `POST /api/agent/memory` menerima `cluster_id` dan `domain`; lingkup tak dikenal / dua lingkup → 400.
- [x] Panel Memory di Settings: pemilih lingkup, hanya pemilih yang dipilih yang tampil, badge lingkup pada catatan.

### Docs
- [x] ADR 0082, glossary **Agent Memory** di CONTEXT.md, README (EN/ID), user-guide (EN/ID), CHANGELOG, `docs/agents/e2e.md`.

### Pengujian
- [x] `go test ./internal/store/ ./internal/web/` — recall, scope, pemotongan, API.
- [x] `cd e2e && npm test` — panel Memory menyimpan catatan berlingkup domain.

## Catatan

- Pemotongan hanya di `truncateToolResult`; byte yang dilaporkan di pemberitahuan adalah byte sebenarnya.
- `SearchFleet` menuntut setiap token cocok, jadi satu kueri per kata kunci (maks 6, stopword EN/ID dibuang).
