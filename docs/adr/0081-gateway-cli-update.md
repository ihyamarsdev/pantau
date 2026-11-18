# 0081. Gateway CLI Update (Ansible Gate & Terraform Gate)

Gateway CLI Update Check (ADR 0080) hanya **membaca**: ia memberi tahu bahwa CLI gateway sudah usang, tetapi pemasangannya diserahkan ke operator. ADR ini menambahkan langkah berikutnya: **Gateway CLI Update**, yang memasang versi baru langsung dari panel gate.

## Konteks dan Masalah

1. **Operator tetap harus masuk ke server.** Setelah "Update available", operator masih harus SSH, mengunduh rilis, memverifikasi checksum, mengekstrak, dan mengganti biner — dengan `sudo` bila menimpa `/usr/bin`. Langkah itu yang paling mudah salah: checksum dilewati, arsip diekstrak ke tempat yang salah, biner versi lama tertinggal di PATH.

2. **Pantau biasanya bukan root.** Di image container dan di banyak server, `terraform` dan `ansible-playbook` dimiliki root di `/usr/bin`. Menimpa mereka bukan hanya butuh hak akses, tetapi juga keliru secara operasional: biner sistem milik manajer paket, dan perubahan itu hilang saat image dibangun ulang.

3. **`ansible-playbook` bukan satu biner.** `ansible-core` adalah paket Python dengan dependensi (jinja2, cryptography, PyYAML, …). Tidak ada satu file yang bisa diunduh dan ditaruh di `bin/`.

4. **Setiap upstream sudah menerbitkan checksum.** HashiCorp menerbitkan `<produk>_<versi>_SHA256SUMS` di sebelah arsip rilis; OpenTofu menerbitkan `tofu_<versi>_SHA256SUMS` di aset rilis GitHub; PyPI menerbitkan `sha256` setiap wheel di JSON proyek. Verifikasi tidak perlu mengarang apa pun — cukup memakai yang sudah ada.

5. **Instalasi yang gagal tidak boleh menghancurkan yang berjalan.** Menimpa biner yang sedang dipakai, atau mengaktifkan virtualenv yang belum teruji, membuat Gate mati sampai operator memperbaikinya.

## Keputusan Arsitektur

1. **Satu istilah: *Gateway CLI Update*.** Definisi glossary di [CONTEXT.md](../../CONTEXT.md): pemasangan versi CLI gateway yang lebih baru ke direktori yang dikelola Pantau, diverifikasi terhadap checksum yang dipublikasikan upstream, tanpa menyentuh salinan sistem. _Avoid_: Self-Update (dipakai untuk rilis Pantau sendiri), package update, apt upgrade.

2. **Direktori yang dikelola Pantau, bukan biner sistem.** `<data>/cli/`:

   ```
   <data>/cli/bin/terraform            biner terraform yang dikelola
   <data>/cli/bin/tofu                 biner tofu yang dikelola
   <data>/cli/venv -> venvs/ansible-2.22.0   symlink ke virtualenv yang aktif
   <data>/cli/venvs/ansible-<versi>    virtualenv satu versi ansible-core
   ```

   `gwupdate.SetManagedRoot` menyetelnya (dipanggil dari `main.go` dengan `<data>/cli`). Tidak butuh root, tidak menyentuh `/usr/bin`, dan hilang bersama data Pantau bila memang diinginkan.

3. **Biner yang dikelola dipakai lebih dulu.** `gwupdate.ResolveBinary` dan `ResolveVenvCommand` mengembalikan biner yang dikelola bila ada, dan baru jatuh ke `exec.LookPath`. `internal/terraform` (Detect, lookTool, Apply/Plan/StateOps) dan `internal/ansible` (Available, Run, Adhoc, facts) memakai keduanya, dan `PATH` anak proses diberi prefiks direktori yang dikelola lewat `gwupdate.PrependPath`. Jadi update langsung berlaku, tanpa restart.

4. **Verifikasi checksum wajib, bukan opsional.** Setiap unduhan dibandingkan dengan manifest upstream. Ketidakcocokan menolak instalasi dan tidak menulis apa pun. Kesalahan checksum dilaporkan beserta potongan hash yang diharapkan dan yang didapat.

5. **Pemasangan atomik.** Biner ditulis ke `.terraform-<versi>.candidate`, **dijalankan** untuk memastikan ia melaporkan versi yang benar, lalu `os.Rename` ke tempat akhir. Virtualenv ansible dibuat di `venvs/ansible-<versi>`, diuji lewat `ansible-playbook --version`, dan **symlink `venv` baru dipindahkan setelah uji itu lulus** — sehingga kegagalan di tengah jalan meninggalkan versi yang berjalan tetap utuh.

6. **ansible-core lewat virtualenv + pip.** Wheel diunduh dari PyPI, dicocokkan dengan `sha256` yang PyPI terbitkan, ditulis ke disk, lalu dipasang dengan `pip install --upgrade` ke virtualenv versi itu. Dependensinya diambil pip dari PyPI (hash-checked oleh pip). `python3 -m venv` dan modul `venv`/`ensurepip` diperlukan; bila tidak ada, pesannya menyebutkan `python3-venv`.

7. **Hanya versi rilis yang diterima.** Versi dipakai untuk menyusun URL unduhan, jadi harus berupa nomor rilis (`1.2.3`); `latest` atau teks bebas ditolak sebelum satu permintaan pun dibuat. Bila badan permintaan tidak menyebut versi, versi rilis terbaru upstream yang dipakai.

8. **REST API, NDJSON seperti instalasi Library Ansible.** `POST /api/ansible/install` dan `POST /api/terraform/install`, badan `{"target":"ansible|terraform|tofu","version":"..."}`. Jawabannya stream NDJSON (`{"log":…}` lalu `{"done":true}` atau `{"done":true,"error":…}`), sehingga UI menunjukkan langkahnya saat berjalan — pola yang sama dengan `libraryStream`. Kegagalan sebelum ada yang ditulis dijawab JSON 400.

9. **Kegagalan dilaporkan apa adanya.** Tidak ada fallback diam-diam ke biner sistem setelah update gagal: kegagalan muncul sebagai error di panel, dan versi lama tetap yang dipakai. Tidak ada penulisan ke disk di luar `<data>/cli/`.

10. **Batas yang disengaja.** Pemicunya tetap manual (tombol di panel gate), konsisten dengan ADR 0078/0080. Tidak ada pemeriksaan atau pemasangan terjadwal. Tidak menyentuh biner sistem, tidak memakai `sudo`, tidak menambah jenis Desired Rule baru.

## Konsekuensi

- Operator memperbarui CLI gateway dari panel, tanpa SSH dan tanpa `sudo`; update langsung dipakai Pantau.
- Tidak ada unduhan yang dipasang tanpa verifikasi checksum, dan tidak ada biner yang menggantikan yang berjalan sebelum ia terbukti bisa dijalankan.
- Instalasi ansible-core butuh `python3-venv` dan akses internet ke PyPI untuk dependensinya; keduanya dilaporkan dengan jelas bila tidak terpenuhi.
- Biner sistem tetap seperti semula: `terraform version` di shell operator tidak berubah, sedangkan Pantau memakai versi yang dikelolanya.
- Update hilang bila direktori data Pantau dihapus — dan itu memang perilaku yang diinginkan.
