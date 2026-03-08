Ini catatan lengkap langkah-langkahnya — simpan ini untuk referensi setiap kali mau jalankan project:

---

## 🚀 Startup Guide — LLM Gateway

### Step 1 — Masuk WSL
```powershell
# Di PowerShell
wsl -d podman-machine-default
```

### Step 2 — Masuk folder project
```bash
cd /mnt/c/Users/Admin/Development/golang/portfolios/libs/llm-gateway
```

### Step 3 — Nyalakan containers
```bash
podman compose up -d
```

Tunggu 15-30 detik sampai PostgreSQL fully up.

### Step 4 — Cek IP containers (bisa berubah setiap restart!)
```bash
podman exec llm-gateway_postgres_1 hostname -I
podman exec llm-gateway_redis_1 hostname -i
```

### Step 5 — Update .env dengan IP terbaru
```bash
nano .env
```

Ubah dua baris ini:
```env
POSTGRES_DSN=postgres://postgres:postgres@<POSTGRES_IP>:5432/llm_gateway?sslmode=disable
REDIS_ADDR=<REDIS_IP>:6379
```

### Step 6 — Jalankan migrations
```bash
make migrate
```

### Step 7 — Jalankan server
```bash
# Pertama kali (seed API key):
RUN_SEED=true go run cmd/gateway/main.go

# Selanjutnya (tanpa seed):
go run cmd/gateway/main.go
```

---

## ⚠️ Catatan Penting

**IP container selalu berubah** setiap kali container di-restart — selalu ulangi Step 4 dan 5 setiap sesi baru.

**Kalau PostgreSQL crash** (mati nyala terus), jalankan:
```bash
podman compose down -v
podman volume prune -f
podman compose up -d
```

**Kalau `make` tidak ada:**
```bash
sudo dnf install make -y
```

**Kalau `podman compose` tidak ada:**
```bash
pip3 install podman-compose
```

---

Simpan ini di README project atau di notes kamu. Siap lanjut Phase 2 sekarang? 🚀