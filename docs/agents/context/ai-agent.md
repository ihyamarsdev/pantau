# AI Agent

Domain reference for the in-process AI agent, its tools, gates, memory and SOUL. Read this when working in `internal/web/agent*.go`, `internal/store/agent*.go` or `internal/inspector/agent*.go`.

**AI Agent**:
Asisten operasi opsional yang berjalan **di dalam proses Pantau** (bukan layanan terpisah): mengobrol lewat chat melayang di pojok kanan bawah, memanggil *Agent Tool* Pantau secara langsung untuk menyelidiki dan memperbaiki Host. Modelnya ditentukan oleh **LLM Provider**; identitas dan aturannya oleh **SOUL**.
_Avoid_: Bot, copilot, auto-fixer

**LLM Provider**:
Endpoint LLM yang dipakai AI Agent, dikonfigurasi di Settings → AI Agent: `anthropic` (Anthropic Messages API) atau `openai` (OpenAI-compatible: OpenAI, OpenRouter, LiteLLM, vLLM, Ollama, atau gateway apa pun lewat *base URL*). Disertai *API key* (write-only) dan *model*. Kosong berarti belum dikonfigurasi.
_Avoid_: Model, engine, backend, LLM server

**Agent Tool**:
Satu fungsi Pantau yang dapat dipanggil AI Agent secara langsung (baca: `list_hosts`, `systemd_detail`, `docker_logs`, dst.; ubah: `systemd_action`, `docker_action`, `inspect_host`, `security_scan`, `exec_command`). Setiap tool ubah menunggu **Agent Approval** dari operator.
_Avoid_: Plugin, skill, command

**Agent Approval**:
Keputusan operator (setujui/tolak) atas satu aksi pengubah yang diminta AI Agent, ditampilkan sebagai kartu persetujuan. Mode `ask` meminta untuk semua perubahan; mode `auto` melewati aksi service/container/scan/inspeksi, tetapi `exec_command` selalu meminta.
_Avoid_: Confirmation dialog, permission prompt

**Agent Memory**:
Kumpulan catatan permanen (maks. 200, masing-masing maks. 500 karakter) yang disimpan AI Agent lewat `memory_save` dan operator lewat Settings. Setiap catatan berlingkup pada paling banyak satu: seluruh fleet, satu **Host**, satu **Kubernetes Cluster**, atau satu **domain** fungsional (security, network, docker, ansible, terraform, kubernetes, database, storage). Hanya catatan yang relevan dengan pertanyaan yang disisipkan ke pesan sistem, bersama ringkasan inventaris yang disebut pertanyaan itu; sisanya dijangkau satu panggilan `memory_list`. Catatan diperlakukan sebagai data, bukan instruksi.
_Avoid_: Chat history, knowledge base, agent training
