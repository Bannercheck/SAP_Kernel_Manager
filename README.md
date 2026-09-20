# sapkernel — SAP Kernel Manager

SAP NetWeaver / S/4HANA kernel güncellemelerini Linux, AIX ve Windows üzerinde güvenli, geri alınabilir ve
tekrarlanabilir şekilde yapan tek binary CLI aracı.

- Mimari: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- İlerleme / yol haritası: [`STATE.md`](STATE.md)
- Geliştirme kuralları: [`CLAUDE.md`](CLAUDE.md)

## Kurulum (hedef sunucuya hiçbir şey kurulmaz)

`make cross` çıktısı olan `dist/` klasörünü sunucuya kopyalayın:

```
dist/sapkernel.sh                  Linux / AIX başlatıcı (doğru binary'yi seçer)
dist/sapkernel.bat                 Windows başlatıcı
dist/bin/sapkernel-linux-amd64     tek başına çalışan yerel binary'ler
dist/bin/sapkernel-linux-ppc64le
dist/bin/sapkernel-aix-ppc64
dist/bin/sapkernel-windows-amd64.exe
```

```sh
./sapkernel.sh status              # <sid>adm olarak çalıştırın
./sapkernel.sh status --sid ABC --output json
```

Örnek ekran: [`docs/examples/status-linux.txt`](docs/examples/status-linux.txt)

## Geliştirme

```sh
make check    # gofmt + vet + test + build
make cross    # 4 platform için dist/ üret
```

Durum: Adım 1 (durum ekranı) tamam; sıradaki adım `STATE.md`'de.
