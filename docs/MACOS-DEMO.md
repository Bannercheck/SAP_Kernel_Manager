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

Deneme sırası (her soruda menüdeki gibi numara girilir; `0` = geri, Enter tek başına seçim yapmaz):
**1** SAP Status → **2** Kernel Backup (`1` evet) → **3** Kernel Files (sunucuyu kendisi tarar, `1` evet)
→ **4** SAP Stop/Start (`2` durdur) → **5** Kernel Update (`1` onay, sonra `1` başlat) → **6** Kernel Rollback (`1` durdur, `1` onay).

Sonuçları dosya sisteminde görebilirsiniz:

```sh
ls -la ~/.kernelman/demo/ABC/SYS/exe/uc/          # linuxx86_64_<tarih> merkezi yedek
ls -la ~/.kernelman/demo/ABC/D00/                 # exe_<tarih> instance yedeği
ls -la ~/.kernelman/demo/ABC/SYS/exe/uc/linuxx86_64/   # kopyalanan SAR'lar
cat    ~/.kernelman/demo/ABC/.kernelman/snapshot.json
```

Tek komut da çalışır: `./kernelman.sh --demo status --output json`. Demoyu sıfırlamak için `rm -rf ~/.kernelman/demo`.

Gerçek Linux/AIX hostunda `demo` yazmadan çalıştırın; program SAP sistemini kendisi bulur.
