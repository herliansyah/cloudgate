# Cloudgate

[English](README.md) | [Bahasa Indonesia](README.id.md)

**Cloudgate** adalah gateway penyimpanan cloud terpadu dan agregator multi-akun yang ringan, dikompilasi menjadi satu binary Go statis mandiri dengan Web UI mode gelap bergaya Google Drive serta antarmuka baris perintah (CLI).

- **Penulis**: Herliansyah
- **Repositori**: [https://github.com/herliansyah/cloudgate](https://github.com/herliansyah/cloudgate)
- **Lisensi**: MIT

---

## Fitur Utama

1. **Agregator Multi-Akun**: Hubungkan beberapa akun pribadi, kerja, maupun server lintas 16 penyedia dan protokol penyimpanan:
   - **Cloud Drive**: Google Drive, Microsoft OneDrive, Dropbox, Box, pCloud, Yandex Disk, Koofr
   - **Cloud Privasi & Terenkripsi**: MEGA, Filen, Proton Drive, PikPak
   - **Object Storage & Protokol Server**: Amazon S3, Backblaze B2, Nextcloud / WebDAV, SFTP, SMB (Windows Share / Samba)
2. **StoragePool Sadar Kapasitas**: Gabungkan beberapa akun penyimpanan menjadi satu pool virtual terpadu di mana penulisan data didistribusikan secara otomatis menggunakan round-robin sadar kapasitas tanpa memecah keutuhan file fisik.
3. **Antrean Task Latar Belakang & Transfer Folder**: Eksekusi antrean FIFO 2-worker persisten di SQLite untuk transfer berkas besar dan pemindahan folder rekursif dengan ketahanan terhadap restart server.
4. **Unduh Langsung dari URL (Remote Ingest)**: Pipeline streaming tanpa disk untuk mengunduh sumber daya web (HTTP/HTTPS) langsung ke penyimpanan cloud mana pun dengan proteksi ketat anti-SSRF.
5. **Replikasi & Sinkronisasi Folder**: Sinkronisasi folder lintas akun dengan mode aditif maupun mirror (berkas yatim dipindahkan ke `RemoteTrash` terisolasi).
6. **Transfer Streaming Tanpa Disk (Zero-Disk)**: Jalankan operasi salin dan pindah file lintas akun langsung melalui streaming in-memory `io.Pipe` tanpa jejak penyimpanan disk sementara di host dan dengan verifikasi semantik pindah yang aman.
7. **RemoteTrash Terisolasi**: Hapus file secara lunak (soft-delete) ke direktori remote terisolasi (`/.cloudgate_trash/`) dengan katalog SQLite lokal yang memetakan path asli untuk pemulihan satu klik yang deterministik.
8. **Arsitektur SQLite Lokal**: Ditenagai SQLite pure-Go (`modernc.org/sqlite`) untuk kompilasi statis tanpa dependensi CGO, dilengkapi indeks pencarian teks lengkap FTS5, rolling window 100 task selesai, dan log audit rolling 100 entri yang atomik.
9. **EncryptedVault & Sinkronisasi GitHub**: Amankan konfigurasi akun dan kredensial dengan enkripsi PBKDF2 + AES-256-GCM, dengan opsi sinkronisasi ke repositori GitHub privat.
10. **Mutex Single-Instance & Pencarian Port**: Secara otomatis mengunci file mutex OS (`~/.config/cloudgate/cloudgate.lock`) untuk mencegah duplikasi proses, dan memindai port yang tersedia mulai dari `5210` (`5210..5300`) mendengarkan pada `0.0.0.0` untuk akses lokal dan LAN.
11. **Pembaruan Mandiri (Self-Updating)**: Pemeriksa rilis resmi GitHub Releases terintegrasi untuk verifikasi checksum SHA-256 dan pembaruan otomatis.
12. **Dukungan Dwibahasa (Dual Language)**: Antarmuka Web UI mendukung pergantian instan antara Bahasa Indonesia dan English, dengan persistensi preferensi lokal.

<!-- Placeholder Screenshot Web UI (misal: docs/assets/cloudgate-preview.png) -->

---

## Memulai Cepat

### 1. Unduh Binary Siap Pakai (Direkomendasikan)

Unduh binary mandiri siap pakai terbaru untuk sistem operasi dan arsitektur Anda di [GitHub Releases](https://github.com/herliansyah/cloudgate/releases):

```bash
# Contoh untuk Linux (berikan izin eksekusi dan jalankan)
chmod +x cloudgate
./cloudgate serve
```

### 2. Atau Kompilasi dari Sumber (Source Code)

Kebutuhan: Go 1.22+

```bash
# Klon repositori
git clone https://github.com/herliansyah/cloudgate.git
cd cloudgate

# Kompilasi binary statis mandiri beserta aset web tersemat
go build -o bin/cloudgate cmd/cloudgate/main.go
```

### 3. Jalankan Server

```bash
# Menjalankan gateway server (default di 0.0.0.0:5210)
./bin/cloudgate serve

# Menggunakan host kustom atau port awal tertentu
./bin/cloudgate serve --host 0.0.0.0 --port 5210
```

Buka antarmuka Web UI di browser Anda:
- Akses Lokal: `http://localhost:5210` (atau `http://127.0.0.1:5210`)
- LAN / Perangkat Lain: `http://<ip-lan-anda>:5210`

> [!IMPORTANT]
> **Keamanan Akses Awal GatewayAuth (MasterPassword)**:
> Cloudgate mewajibkan pembuatan `MasterPassword` saat pertama kali dijalankan. Demi keamanan, inisialisasi password melalui Web UI dibatasi secara ketat hanya untuk loopback (`localhost` / `127.0.0.1`).
> Jika dijalankan di server headless atau VPS remote, konfigurasikan password administratif Anda terlebih dahulu melalui CLI sebelum mengakses Web UI dari jaringan luar:
> ```bash
> ./bin/cloudgate auth setup "master-password-anda"
> ```

---

## Menghubungkan Penyedia Cloud

Cloudgate menghubungkan akun cloud melalui dua metode autentikasi:

1. **Penyedia Kredensial Langsung & Protokol Server** (Instan):
   - **Didukung**: MEGA, Filen, Proton Drive, PikPak, Amazon S3, Backblaze B2, Nextcloud / WebDAV, SFTP, SMB (Windows Share / Samba).
   - **Cara Hubungkan**: Pada antarmuka Web UI, klik **"+ Tambah Akun"** / **"+ Add Account"**, pilih penyedia, lalu masukkan email/kata sandi, kunci API, atau alamat server Anda secara langsung. Tidak memerlukan pendaftaran aplikasi developer eksternal.
2. **Penyedia Delegasi OAuth** (Persetujuan Izin Akun):
   - **Didukung**: Google Drive, Microsoft OneDrive, Dropbox, Box.
   - **Cara Hubungkan**: Memerlukan OAuth Client ID & Client Secret untuk verifikasi izin browser. Ikuti panduan langkah demi langkah untuk Google Drive di bawah ini sebagai referensi.

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
cloudgate auth status     Periksa status proteksi GatewayAuth
cloudgate auth setup <pw>  Setel MasterPassword awal melalui terminal
cloudgate auth reset       Reset MasterPassword dan kunci ulang ke status setup awal
cloudgate version          Tampilkan informasi versi dan pembuat
cloudgate help             Tampilkan bantuan perintah CLI
```

---

## Arsitektur & Struktur Direktori

```
.
├── cmd/cloudgate/         # Titik masuk utama binary dan perintah CLI
├── pkg/
│   ├── auth/              # Pembuatan URL persetujuan OAuth2, pertukaran token, & MasterPassword bcrypt
│   ├── config/            # Resolver path konfigurasi & lockfile single-instance
│   ├── db/                # Skema SQLite pure-Go, migrasi, & pencarian FTS5
│   ├── server/            # REST API, server aset statis, & manajemen port
│   ├── storage/           # Vendor driver (GDriveDriver), StoragePool, Trash, Transfer
│   ├── sync/              # Sinkronisasi EncryptedVault ke GitHub
│   ├── updater/           # Pemeriksaan versi & pembaruan dari GitHub Releases
│   └── vault/             # Enkripsi vault PBKDF2 + AES-256-GCM
├── web/                   # Frontend SPA tersemat (Material Design 3 mode gelap)
├── docs/
│   ├── adr/               # Catatan Keputusan Arsitektur (ADR 0001 - 0024)
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
