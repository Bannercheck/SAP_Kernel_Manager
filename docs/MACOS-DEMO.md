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

Deneme sırası (sorulara tuşla cevap verilir: `Y` evet, `N` hayır, `M` ana menü; Enter tek başına seçim yapmaz):
**1** SAP Status → **2** Kernel Backup (`Y`) → **4** Kernel File Transfer (sunucuyu kendisi tarar, `Y`)
→ **5** SAP Stop/Start (`K` durdur) → **6** Kernel Update (`Y` onay, sonra `Y` başlat) → **7** Kernel Rollback (`Y` durdur, `Y` onay).
**8** Send to Other Servers gerçek ssh/scp ister. **3** Kernel Download isteğe bağlıdır: gerçek S-user ve SAP erişimi ister; erişim yoksa kendisi söyler ve geri döner.

Sonuçları dosya sisteminde görebilirsiniz:

```sh
ls -la ~/.kernelman/demo/ABC/SYS/exe/uc/          # linuxx86_64_<tarih> merkezi yedek
ls -la ~/.kernelman/demo/ABC/D00/                 # exe_<tarih> instance yedeği
ls -la ~/.kernelman/demo/ABC/SYS/exe/uc/linuxx86_64/   # kopyalanan SAR'lar
cat    ~/.kernelman/demo/ABC/.kernelman/snapshot.json
```

Tek komut da çalışır: `./kernelman.sh --demo status --output json`. Demoyu sıfırlamak için `rm -rf ~/.kernelman/demo`.

Gerçek Linux/AIX hostunda `demo` yazmadan çalıştırın; program SAP sistemini kendisi bulur.
