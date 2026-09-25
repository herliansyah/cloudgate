# Cloudgate

[English](README.md) | [Bahasa Indonesia](README.id.md)

**Cloudgate** adalah gateway penyimpanan cloud terpadu dan agregator multi-akun yang ringan, dikompilasi menjadi satu binary Go statis mandiri dengan Web UI mode gelap bergaya Google Drive serta antarmuka baris perintah (CLI).

- **Penulis**: Herliansyah
- **Repositori**: [https://github.com/herliansyah/cloudgate](https://github.com/herliansyah/cloudgate)
- **Lisensi**: MIT

---

## Fitur Utama

1. **Agregator Multi-Akun**: Hubungkan beberapa akun penyimpanan cloud pribadi dan kerja lintas penyedia (Google Drive, OneDrive, Dropbox, Box, Mega, pCloud, Nextcloud/WebDAV, S3, Koofr, Yandex Disk).
2. **StoragePool Sadar Kapasitas**: Gabungkan beberapa akun penyimpanan menjadi satu pool virtual terpadu di mana penulisan data didistribusikan secara otomatis menggunakan round-robin sadar kapasitas tanpa memecah keutuhan file fisik.
3. **Transfer Streaming Tanpa Disk (Zero-Disk)**: Jalankan operasi salin dan pindah file lintas akun langsung melalui streaming in-memory `io.Pipe` tanpa jejak penyimpanan disk sementara di host dan dengan verifikasi semantik pindah yang aman.
4. **RemoteTrash Terisolasi**: Hapus file secara lunak (soft-delete) ke direktori remote terisolasi (`/.cloudgate_trash/`) dengan katalog SQLite lokal yang memetakan path asli untuk pemulihan satu klik yang deterministik.
5. **Arsitektur SQLite Lokal**: Ditenagai SQLite pure-Go (`modernc.org/sqlite`) untuk kompilasi statis tanpa dependensi CGO, dilengkapi indeks pencarian teks lengkap FTS5 dan log audit rolling 100 entri yang atomik.
6. **EncryptedVault & Sinkronisasi GitHub**: Amankan konfigurasi akun dan kredensial dengan enkripsi PBKDF2 + AES-256-GCM, dengan opsi sinkronisasi ke repositori GitHub privat.
7. **Mutex Single-Instance & Pencarian Port**: Secara otomatis mengunci file mutex OS (`~/.config/cloudgate/cloudgate.lock`) untuk mencegah duplikasi proses, dan memindai port yang tersedia mulai dari `5210` (`5210..5300`) mendengarkan pada `0.0.0.0` untuk akses lokal dan LAN.
8. **Pembaruan Mandiri (Self-Updating)**: Pemeriksa rilis resmi GitHub Releases terintegrasi untuk verifikasi checksum SHA-256 dan pembaruan otomatis.
9. **Dukungan Dwibahasa (Dual Language)**: Antarmuka Web UI mendukung pergantian instan antara Bahasa Indonesia dan English, dengan persistensi preferensi lokal.

---

## Memulai Cepat

### Kompilasi dari Sumber (Source Code)

```bash
# Klon repositori
git clone https://github.com/herliansyah/cloudgate.git
cd cloudgate

# Kompilasi binary statis mandiri beserta aset web tersemat
go build -o bin/cloudgate cmd/cloudgate/main.go
```

### Jalankan Server

```bash
# Menjalankan gateway server (default di 0.0.0.0:5210)
./bin/cloudgate serve

# Menggunakan host kustom atau port awal tertentu
./bin/cloudgate serve --host 0.0.0.0 --port 5210
```

Buka antarmuka Web UI di browser Anda:
- Akses Lokal: `http://localhost:5210` (atau `http://127.0.0.1:5210`)
- LAN / Perangkat Lain: `http://<ip-lan-anda>:5210`

---

## Panduan Integrasi Google Drive

Untuk menghubungkan akun Google Drive pribadi atau perusahaan:

### 1. Aktifkan Google Drive API (Wajib)
Pada proyek Google Cloud Anda, Google Drive API harus diaktifkan:
- Kunjungi [Google Drive API Overview](https://console.developers.google.com/apis/api/drive.googleapis.com/overview).
- Klik **"ENABLE"** (Aktifkan).

### 2. Konfigurasi Layar Persetujuan OAuth (OAuth Consent Screen)
- Buka [Google Cloud Console - OAuth Consent Screen](https://console.cloud.google.com/apis/credentials/consent).
- Jika status publikasi masih **Testing**, tambahkan alamat email Google Anda di bagian **Test users**.

### 3. Buat Kredensial OAuth 2.0
- Buka [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials).
- Klik **Create Credentials** &rarr; **OAuth client ID**.
- Pilih **Web application** (atau **Desktop app**).
- Pada bagian **Authorized redirect URIs**, tambahkan:
  ```
  http://localhost:5210/api/auth/google/callback
  ```
- Salin **Client ID** dan **Client Secret** yang dihasilkan.

### 4. Hubungkan di Cloudgate
- Pada Web UI Cloudgate, klik **"+ Tambah Akun"** / **"+ Add Account"**.
- Pilih **Google Drive**.
- Tempelkan **Client ID** dan **Client Secret** Anda.
- Klik **"Masuk dengan Akun Google"** dan setujui akses izin.
- Cloudgate akan menyelesaikan pertukaran token, membaca detail akun, mengambil kuota penyimpanan riil, dan menampilkan daftar file Anda.

---

## Referensi Perintah CLI

```
cloudgate                  Jalankan server pada 0.0.0.0:5210 dan buka Web UI
cloudgate serve            Jalankan server di terminal saat ini
  --host string            IP host yang di-bind (default "0.0.0.0")
  --port int               Port awal (default 5210, mencari otomatis jika terpakai)
cloudgate accounts         Daftar akun penyimpanan cloud yang terhubung
cloudgate audit            Lihat 100 aktivitas log audit terbaru
cloudgate version          Tampilkan informasi versi dan pembuat
cloudgate help             Tampilkan bantuan perintah CLI
```

---

## Arsitektur & Struktur Direktori

```
.
├── cmd/cloudgate/         # Titik masuk utama binary dan perintah CLI
├── pkg/
│   ├── auth/              # Pembuatan URL persetujuan OAuth2 & pertukaran token
│   ├── config/            # Resolver path konfigurasi & lockfile single-instance
│   ├── db/                # Skema SQLite pure-Go, migrasi, & pencarian FTS5
│   ├── server/            # REST API, server aset statis, & manajemen port
│   ├── storage/           # Vendor driver (GDriveDriver), StoragePool, Trash, Transfer
│   ├── sync/              # Sinkronisasi EncryptedVault ke GitHub
│   ├── updater/           # Pemeriksaan versi & pembaruan dari GitHub Releases
│   └── vault/             # Enkripsi vault PBKDF2 + AES-256-GCM
├── web/                   # Frontend SPA tersemat (Material Design 3 mode gelap)
├── docs/
│   ├── adr/               # Catatan Keputusan Arsitektur (ADR 0001 - 0019)
│   └── agents/            # Konvensi domain dan spesifikasi triage agent
├── CONTEXT.md             # Kosakata kanonikal dan glosarium domain (English)
├── CONTEXT.id.md          # Kosakata kanonikal dan glosarium domain (Bahasa Indonesia)
└── AGENTS.md              # Aturan perilaku agent dan peta skill
```

---

## Pengujian (Testing)

Jalankan rangkaian pengujian otomatis lengkap:

```bash
go test -v ./pkg/...
```
