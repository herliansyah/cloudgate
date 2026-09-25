# Cloudgate

[English](CONTEXT.md) | [Bahasa Indonesia](CONTEXT.id.md)

Gateway penyimpanan cloud terpadu dan agregator multi-akun yang ringan, dikompilasi menjadi satu biner mandiri Go.

## Bahasa Domain (Ubiquitous Language)

**RemoteAccount**:
Koneksi terkonfigurasi dan terautentikasi ke penyedia penyimpanan cloud tertentu dengan kredensial pengguna unik, sebuah AccountPrincipal, alias tampilan opsional yang ditentukan pengguna, serta pelacakan kapasitas penyimpanan.
_Hindari_: Drive connection, cloud user, session

**AccountPrincipal**:
Identitas eksternal kanonikal pengguna atau sumber daya target yang terkait dengan RemoteAccount (misalnya alamat email terotentikasi untuk layanan OAuth/Mega, `username@host` untuk WebDAV, atau `bucket_name` untuk S3).
_Hindari_: ID pengguna, kunci internal penyedia, nomor akun

**Provider**:
Layanan penyimpanan cloud pihak ketiga yang didukung (misalnya: Google Drive, OneDrive, Dropbox, S3, Mega).
_Hindari_: Vendor, cloud host

**StoragePool**:
Agregasi virtual dari RemoteAccount terpilih di mana penulisan file didistribusikan menggunakan round-robin yang sadar kapasitas (capacity-aware).
_Hindari_: Bucket, kluster, drive group

**VirtualFile**:
Representasi file logis di dalam StoragePool yang memetakan langsung ke file fisik utuh pada RemoteAccount tertentu.
_Hindari_: Chunk, striping block, virtual link

**TrashRecord**:
Entri item yang dihapus secara lunak (soft-deleted) yang dicatat di katalog lokal dengan mempertahankan RemoteAccount asal dan path remote untuk pemulihan yang tepat.
_Hindari_: Recycle bin entry, tombstone

**AuditEvent**:
Catatan transaksi tindakan pengguna atau sistem yang tidak dapat diubah (immutable), disimpan dalam jendela bergulir 100 operasi terakhir.
_Hindari_: Log entry, history item

**EncryptedVault**:
Bundel terproteksi secara kriptografis (AES-256-GCM) yang memuat kredensial akun dan konfigurasi sensitif, cocok untuk sinkronisasi aman ke repositori privat GitHub.
_Hindari_: Plain config, backup file

**RemoteTrash**:
Direktori terisolasi khusus (`/.cloudgate_trash/`) yang dikelola pada setiap RemoteAccount untuk menampung file yang dihapus lunak hingga dipulihkan atau dibersihkan permanen.
_Hindari_: Provider recycle bin, system trash

**AllocationPolicy**:
Aturan tata kelola penempatan file di dalam StoragePool, yang mengevaluasi kuota tersedia dan konektivitas akun sebelum penugasan penulisan.
_Hindari_: Load balancer, placement algorithm

**TransferSession**:
Saluran streaming dalam memori (in-memory pipeline) yang memediasi operasi salin atau pindah file lintas akun menggunakan piped buffer dan verifikasi pasca-transfer.
_Hindari_: Sync task, file job

**MetadataIndex**:
Cache lokal berbasis SQLite untuk hierarki file dan atribut yang mendukung Pencarian Teks Lengkap (FTS5) di seluruh akun tanpa memanggil API live provider secara berulang.
_Hindari_: File list cache, catalog dump

**RollingAuditLog**:
Log audit terbatas 100 entri yang disimpan dalam SQLite yang melacak manipulasi file oleh sistem dan pengguna dengan penghapusan atomik untuk entri melampaui rekor ke-100.
_Hindari_: Audit table, system journal

**InstanceLock**:
Mutex file lokal yang mencatat PID dan port listening untuk mencegah eksekusi bersamaan beberapa proses Cloudgate pada mesin yang sama.
_Hindari_: PID file, process guard

**ReleasePackage**:
Aset rilis biner resmi yang dihosting di GitHub Releases, digunakan untuk pemeriksaan versi otomatis dan pembaruan mandiri.
_Hindari_: Update bundle, binary patch

**OAuthAuthorization**:
Jabat tangan protokol yang memberikan Cloudgate access token dan refresh token dari penyedia identitas pihak ketiga tanpa menyimpan kata sandi plaintext.
_Hindari_: Login session, API handshake

**DirectCredentialAuth**:
Alur autentikasi berfriksi tinggi di mana Cloudgate mengelola kredensial mentah pengguna (seperti username/email dan kata sandi utama) untuk menegosiasikan sesi langsung dengan penyedia yang tidak memiliki delegasi OAuth standar (khususnya MEGA), dengan risiko operasional pemblokiran fraud pihak ketiga, penandaan IP, dan tantangan keamanan.
_Hindari_: Password login, basic auth, direct connection

**VendorDriver**:
Adapter tipis di atas `rclone/fs.Fs` tersemat yang mengimplementasikan antarmuka Driver seragam (`About`, `List`, `Get`, `Put`, `Delete`, `Move`, `Mkdir`) dengan mendelegasikan upload chunked, retry, rate-limiting, dan keunikan provider ke rclone tanpa memerlukan eksekusi binary eksternal.
_Hindari_: Raw REST client, client wrapper, connector plugin

**CredentialPersistence**:
Mekanisme berbasis SQLite lokal yang menyimpan refresh token dan client secret OAuth secara aman di kolom `accounts.credentials`, dan saat restart, merehidrasi VendorDriver aktif dengan menyuntikkan kredensial tersebut ke konfigurasi `rclone/fs` dalam memori tanpa pernah menulis file `rclone.conf` persisten ke disk.
_Hindari_: Token cache, session store, rclone.conf file

**AccountDisconnection**:
Proses pelepasan terkoordinasi yang mencabut RemoteAccount, mengeluarkan Driver aktifnya dari semua StoragePool, membersihkan entri MetadataIndex terkait, dan menghapus kredensial yang tersimpan.
_Hindari_: Account removal, unmount

**RemoteAccountUpdate**:
Proses memodifikasi parameter konfigurasi non-sensitif (seperti alias tampilan, folder root mount, dan kuota yang dialokasikan) dari RemoteAccount yang ada, memicu pembatalan cache selektif tanpa memutus autentikasi provider yang aktif.
_Hindari_: Account edit, profile update

**StorageHub**:
Pusat kendali dan telemetri yang didedikasikan untuk mengelola seluruh RemoteAccount yang terhubung, mengagregasikan distribusi kuota, menyediakan diagnostik koneksi, dan memicu sinkronisasi menyeluruh.
_Hindari_: Accounts page, settings screen, connection list

**UnifiedExplorer**:
Antarmuka penjelajahan dan manipulasi file terkonsolidasi yang menyajikan hierarki virtual gabungan di seluruh RemoteAccount yang aktif sebagai satu sistem penyimpanan mulus.
_Hindari_: Drive view, file manager window, bucket explorer

**ConnectionPipeline**:
Protokol onboarding terstruktur 5 fase untuk menghubungkan RemoteAccount baru: Pemilihan Provider → Autentikasi → Pengujian Diagnostik Koneksi → Pengambilan Kuota Penyimpanan → Persistensi Akun.
_Hindari_: Connect wizard, add account popup, login form

**AccountIntegrationState**:
Flag ketersediaan operasional (`enabled` atau `disabled`) dari RemoteAccount terautentikasi, memungkinkan akun dikecualikan sementara dari distribusi penulisan dan pool sinkronisasi tanpa memutus kredensial atau menghapus metadata tersimpan.
_Hindari_: Account pause, sleep mode, disable toggle

**SyncSession**:
Rutinitas rekonsiliasi manual atau terjadwal ("Sync Now") yang menanyakan live provider driver untuk memperbarui statistik kuota, memverifikasi keterjangkauan jarak jauh, dan menyinkronkan MetadataIndex lokal.
_Hindari_: Re-scan, refresh task, cache reload

**StarredRecord**:
Penanda yang dibubuhi pengguna mengarah ke VirtualFile atau path tertentu yang disimpan dalam katalog SQLite lokal untuk penemuan instan lintas akun.
_Hindari_: Favorite item, pinned file, bookmark

**ShareLink**:
URL akses langsung atau sementara yang dibuat untuk VirtualFile, memanfaatkan kapabilitas berbagi bawaan provider atau gateway streaming terautentikasi Cloudgate.
_Hindari_: Public URL, download link

**GatewayAuth**:
Sistem kontrol akses lokal yang membatasi akses ke antarmuka web Cloudgate dan endpoint REST API hingga sesi yang valid terbentuk.
_Hindari_: Login page, user account system, web guard

**MasterPassword**:
Rahasia utama yang dikonfigurasi pengguna, di-hash secara kriptografis dan disimpan dalam database SQLite lokal, digunakan untuk membuka kunci GatewayAuth dan memperoleh kendali administratif penuh atas Cloudgate.
_Hindari_: User password, login credential, account pin
