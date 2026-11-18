# Memori Agent Berlingkup, Recall Lokal, dan Prompt yang Lebih Kecil

Status: implemented (ADR 0082)

Dasar: [ADR-0066](../../docs/adr/0066-hermes-ai-agent-and-mcp-tools.md) (memori agent), [ADR-0073](../../docs/adr/0073-in-process-ai-agent.md) (agen in-process), [ADR-0074](../../docs/adr/0074-agent-tool-coverage-of-pantau-actions.md) (cakupan tool), CONTEXT.md.

Satu catatan **Agent Memory** dapat dilingkupkan ke seluruh fleet, satu **Host**, satu **Kubernetes Cluster**, atau satu **domain** fungsional. Hanya catatan dan fakta inventaris yang relevan dengan pertanyaan yang masuk ke prompt, dan hasil tool yang besar dipotong dengan pemberitahuan yang jujur.

## Istilah domain

**Agent Memory Scope**:
Satu lingkup yang dibawa sebuah catatan **Agent Memory**: seluruh fleet, satu **Host**, satu **Kubernetes Cluster**, atau satu **domain** fungsional (`security`, `network`, `docker`, `ansible`, `terraform`, `kubernetes`, `database`, `storage`). Sebuah catatan membawa paling banyak satu lingkup.
_Avoid_: Memory tag, note label, note folder

## Konteks dan Masalah

1. **Seluruh tabel memori masuk ke setiap prompt.** ADR-0066 menempelkan catatan ke pesan sistem setiap giliran, terbaru lebih dulu, sampai 6000 byte. Catatan yang tidak relevan tetap dibayar di setiap giliran, dan catatan tentang host lain bisa terbaca sebagai fakta tentang host yang sedang dibahas.
2. **Lingkup hanya "fleet" atau "satu host".** `agent_memory.host_id` punya dua keadaan saja, padahal ada klaster dan ada area fungsional yang berulang di banyak host.
3. **Agen tidak bisa menjawab dari data lokal.** Fleet Search sudah menyimpan paket, container, dan domain hasil inspeksi, tetapi harus dipanggil tool untuk setiap pertanyaan sederhana.
4. **Hasil tool dipotong diam-diam.** Pemotongan ke 24 KiB dengan penanda `…[truncated]` membuang ujung log — tempat error berada — tanpa memberi tahu cara mengambil sisanya.
5. **Riwayat tumbuh tanpa batas.** Setiap hasil tool utuh dikirim lagi pada setiap permintaan model berikutnya.

## Keputusan Arsitektur

### 1. Skema additive: `cluster_id` dan `domain`

- `agent_memory.cluster_id` INTEGER REFERENCES kube_clusters(id) ON DELETE CASCADE; `agent_memory.domain` TEXT NOT NULL DEFAULT ''.
- `AgentMemoryScope{HostID, ClusterID, Domain}` di `internal/store`; `normalizeScope` menolak dua lingkup sekaligus (`ErrAgentMemoryScope`) dan menormalkan huruf besar domain ke huruf kecil (`ErrAgentMemoryDomain`).
- `AgentMemoryDomains` = security, network, docker, ansible, terraform, kubernetes, database, storage.
- Catatan lama (tanpa lingkup) tetap berarti seluruh fleet. Snapshot menyalin kedua kolom.

### 2. Recall: skor relevansi, bukan dump

- `RecallAgentMemory(scope, query, limit)`: lingkup yang cocok +8 (host/cluster), domain yang cocok +5, catatan seluruh fleet +1, kata yang sama +2 per kata; catatan tentang host atau cluster lain dibuang. Skor <2 berarti tidak ada kaitan.
- `RecallContext(scope, query, limit)` mengembalikan catatan + `RecallFact` (kind, name, hosts, count) dari inventaris.
- Satu pencarian Fleet Search **per kata kunci** yang layak, bukan satu untuk seluruh pertanyaan: `SearchFleet` menuntut setiap token cocok, sehingga "nginx di alpha" mengembalikan nol. Maks 6 kata kunci, stopword dibuang (EN dan ID).

### 3. Lingkup dibaca dari pertanyaan

- `memoryScopeOf(question)`: cari nama host yang disebut, lalu kata domain; host lebih sempit daripada domain, jadi satu saja yang dipakai.
- Prompt (`agentMemoryPrompt(question)`): catatan yang cocok + fakta inventaris + "(N more notes not shown: use `memory_list`)".
- Tidak ada catatan yang cocok tetapi ada fakta → fakta tetap masuk; itu jawaban atas pertanyaannya.

### 4. Potongan hasil tool diumumkan

- `truncateToolResult(tool, s)`: lebih dari 24 KiB → kepala + ekor + pemberitahuan yang menyebut jumlah byte yang dilewati dan hint per tool (`docker_logs` → `docker logs --tail`, `read_file` → `offset`, `disk_usage` → satu subdirektori, dst.).
- `shrinkOldToolResults(msgs)`: hasil tool yang lebih tua dari 4 terbaru dipangkas ke 2 KiB sebelum tiap panggilan model; pemotongan announced.
- `callAgentTool` **tidak** memotong lagi: pemotongan hanya di satu tempat, sehingga hitungan byte jujur dan ujung log sampai ke model.

### 5. Operator memakai lingkup yang sama

- `POST /api/agent/memory` menerima `host_id`, `cluster_id` atau `domain` (paling banyak satu).
- Panel Memory di Settings punya pemilih lingkup; hanya pemilih yang dipilih yang ditampilkan.

## Di luar cakupan

Embedding/vector store, RAG di luar SQLite, ringkasan otomatis catatan dengan LLM, Billing token per recall, sinkronisasi catatan antar-instance.

## Aturan pengujian

Agen mengimplementasikan dan menulis unit test (`go test`) dan e2e (`cd e2e && npm test`): recall hanya menyuntik catatan yang cocok, fakta bertahan pada pertanyaan multi-kata, satu tool result dipotong satu kali, dan operator bisa menyimpan catatan berlingkup domain dari panel.
