# 0082. Memori agent berlingkup, recall lokal, dan prompt yang lebih kecil

Memperluas [ADR-0066](0066-hermes-ai-agent-and-mcp-tools.md) (memori agent) dan [ADR-0073](0073-in-process-ai-agent.md) (agen in-process): satu catatan **Agent Memory** kini dapat dilingkupkan ke sebuah **Host**, sebuah **Kubernetes Cluster**, atau sebuah **domain** fungsional — bukan hanya ke seluruh fleet — dan prompt hanya membawa catatan serta fakta inventaris yang memang relevan dengan pertanyaan, bukan seluruh tabel.

## Konteks dan Masalah

1. **Seluruh tabel memori masuk ke setiap prompt.** ADR-0066 menempelkan catatan memori ke pesan sistem setiap giliran chat, terbaru lebih dulu, sampai 6000 byte. Dengan 200 catatan, prompt penuh berisi catatan yang tidak ada hubungannya dengan pertanyaan: biaya token untuk setiap giliran, dan catatan yang tidak relevan bisa menyesatkan model (catatan tentang host B terbaca sebagai fakta tentang host A).

2. **Lingkup catatan hanya "fleet" atau "satu host".** `agent_memory.host_id` hanya punya dua keadaan. Kenyataan operasional Pantau tidak seperti itu: ada klaster (`kube_clusters`), dan ada area fungsional yang berulang (security, network, docker, ansible, terraform, kubernetes, database, storage) — beberapa host di dalamnya, catatan yang sama berlaku untuk semuanya.

3. **Agen tidak bisa menjawab dari data lokal tanpa SSH.** Fleet Search sudah menyimpan paket, container, dan domain hasil inspeksi terakhir di SQLite, tetapi agen harus memanggil `fleet_search` untuk setiap hal. Pertanyaan yang paling sering ("host mana yang menjalankan nginx?") bisa dijawab dari data yang sudah ada, tanpa satu pun koneksi SSH.

4. **Hasil tool yang besar dipotong diam-diam.** Hasil tool dipotong ke 24 KiB dengan penanda `…[truncated]`: model kehilangan ujung log — tempat error dan jejak kegagalan berada — dan tidak diberi tahu cara mengambil sisa isinya.

5. **Riwayat chat tumbuh tanpa batas.** Setiap hasil tool utuh disimpan di riwayat dan dikirim lagi pada setiap permintaan model berikutnya, sampai kompresi dipakai.

## Keputusan Arsitektur

1. **Satu catatan, satu lingkup.** Tabel `agent_memory` mendapat dua kolom additive: `cluster_id` (FK ke `kube_clusters`, `ON DELETE CASCADE`) dan `domain` (`TEXT`, default `''`). `AgentMemoryScope` di `internal/store` menyatakan satu dari tiga; `normalizeScope` menolak dua lingkup sekaligus dan menormalkan huruf besar domain ke huruf kecil. Skema bersifat additive: catatan lama (tanpa lingkup) tetap valid dan berarti seluruh fleet.

2. **Recall, bukan dump.** `RecallAgentMemory` (store) menilai setiap catatan terhadap pertanyaan: lingkup yang cocok bernilai paling tinggi, kata yang sama menambah skor, catatan tentang host lain dibuang. `RecallContext` menambahkan **fakta inventaris**: satu pencarian Fleet Search per kata kunci yang layak, bukan satu untuk seluruh pertanyaan — `SearchFleet` menuntut setiap token cocok, sehingga "nginx di alpha" akan mengembalikan nol. Hasil diurutkan menurut skor relevansi, bukan hanya waktu.

3. **Lingkup dibaca dari pertanyaannya.** `memoryScopeOf` (web) mencari nama host yang disebut dan kata domain ("security", "docker", …) dalam pertanyaan, lalu mengambil hanya lingkup itu. Lingkup mempersempit, tidak pernah memperluas.

4. **Fakta berdiri sendiri.** Bila tidak ada catatan yang cocok tetapi ada inventaris yang cocok, fakta tetap masuk ke prompt: itulah jawaban atas pertanyaannya. Bagian Memory juga mencatat berapa catatan lain yang ada (`memory_list` untuk sisanya).

5. **Potongan hasil tool diumumkan.** `truncateToolResult` memotong hasil melebihi 24 KiB menjadi kepala + ekor + pemberitahuan terstruktur yang menyebut berapa byte yang dilewati dan **bagaimana** mengambil sisanya — hint spesifik per tool (`docker_logs` → `docker logs --tail`, `read_file` → `offset`, `disk_usage` → satu subdirektori, dst.). Pemotongan tidak pernah senyap. Hasil tool yang lebih tua dari empat terbaru dipangkas ke 2 KiB sebelum setiap panggilan model, dan itu pun diberi tahu.

6. **Pemotongan hanya di satu tempat.** `callAgentTool` tidak memotong lagi hasil tool; pemotongan terjadi hanya di `truncateToolResult`, sehingga hitungan byte pada pemberitahuan jujur dan ujung log benar-benar sampai ke model.

7. **Operator memakai lingkup yang sama.** `POST /api/agent/memory` menerima `cluster_id` dan `domain` seperti `memory_save`, dan panel Memory di Settings punya pemilih lingkup (fleet / satu host / satu cluster / satu domain). Kolom `cluster_id`/`domain` bersifat additive; catatan lama tetap terbaca.

## Konsekuensi

- Prompt jauh lebih kecil dan relevan: hanya catatan yang cocok dengan pertanyaan, ditambah fakta inventaris yang dibaca dari SQLite tanpa SSH.
- Skema `agent_memory` berubah secara additive (`cluster_id`, `domain`); snapshot menyalin kedua kolom, dan catatan lama tanpa lingkup tetap berlaku untuk seluruh fleet.
- Pencarian inventaris memakai satu kueri per kata kunci: lebih banyak kueri SQLite, semuanya baca lokal dan tanpa SSH.
- Pemberitahuan pemotongan menambah sedikit byte ke prompt, tetapi mencegah model menyimpulkan bahwa ia sudah melihat seluruh keluaran.
