# KernelMan — macOS demo

Mac'te SAP yoktur; bu paket programın **demo modunu** denemeniz içindir. Demo, `~/.kernelman/demo` altında sahte ama tutarlı
bir SAP hostu kurar (kernel dizini, bugün tarihli SAR dosyaları, durup kalkan sapcontrol simülasyonu). Dosya işlemleri
(kopyalama, yedek, listeleme) gerçektir; sapcontrol / SAPCAR / disp+work simüle edilir.

## Çalıştırma

```sh
unzip kernelman-macos-demo.zip && cd macos
xattr -dr com.apple.quarantine .        # Gatekeeper: indirilen dosya kilidini kaldır
chmod +x kernelman.sh bin/*
./kernelman.sh demo                       # menü, DEMO rozetiyle
```

Deneme sırası: **1** SAP Status → **2** Kernel Backup → **3** Kernel Files (dizin sorusuna Enter: `~/.kernelman/demo/download`)
→ **4** `K` (durdur) → **5** Kernel Update (Enter, sonra `S` ile başlat) → **6** Kernel Rollback.

Sonuçları dosya sisteminde görebilirsiniz:

```sh
ls -la ~/.kernelman/demo/ABC/SYS/exe/uc/          # exe_<tarih> yedekleri
ls -la ~/.kernelman/demo/ABC/SYS/exe/uc/linuxx86_64/   # kopyalanan SAR'lar
cat    ~/.kernelman/demo/ABC/.kernelman/snapshot.json
```

Tek komut da çalışır: `./kernelman.sh --demo status --output json`. Demoyu sıfırlamak için `rm -rf ~/.kernelman/demo`.

Gerçek Linux/AIX hostunda `demo` yazmadan çalıştırın; program SAP sistemini kendisi bulur.
