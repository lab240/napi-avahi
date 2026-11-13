# napi-avahi

Найти NAPI в сети, не зная её IP.

Пакет даёт плате mDNS-имя `napi<SoC>-<id>.local`, например `napi3308-79df.local`
(NAPI-C, RK3308) или `napi3568-1c2e.local` (NAPI2, RK3568), и объявляет службу `_napi._tcp`
с полями TXT.

`id`: 4 знака md5 от серийного номера чипа (`/proc/device-tree/serial-number`). MAC для этого
не годится: Armbian выводит его из части серийника, и у разных плат он бывает одинаковым (как и
конец серийника). Если серийника нет, берётся конец MAC.

Поля TXT:

| поле | пример |
|------|--------|
| `model` | `NAPI-C` |
| `soc` | `rk3308` |
| `os` | `armbian-26.11.0-trunk.117`, `napilinux-1.4` |
| `role` | `donoff,mgate` (задаёте вы) |
| `mac` | `d2:a9:fd:81:b9:df` |
| `serial` | `9e098bbcae8b6599` |
| `kernel` | `6.18.62-current-rockchip64` |

Имя и поля обновляются при каждой загрузке, до запуска avahi. Системный hostname не меняется.

## Установка на NAPI

```sh
curl -fsSL https://repo.napilab.ru/napilab.gpg | gpg --dearmor > /usr/share/keyrings/napilab.gpg
echo "deb [arch=arm64 signed-by=/usr/share/keyrings/napilab.gpg] https://repo.napilab.ru stable main" > /etc/apt/sources.list.d/napilab.list
apt update && apt install napi-avahi
```

## NapiLinux (Yocto)

Рецепт `yocto/napi-avahi_git.bb`: положить в слой, добавить в образ
`IMAGE_INSTALL:append = " napi-avahi"`. Тянет `avahi-daemon` и `avahi-utils`, ставит
скрипт, включает `napi-avahi.service`, `/etc/napi/avahi.conf` помечен как `CONFFILES`.
Скрипт проверен на busybox. Перед релизом закрепить `SRCREV`.

### NapiLinux без рецепта (старые образы)

Архив `napi-avahi-<версия>.tar.gz` (`make tar`): скопировать на плату и под root

```sh
tar xzf napi-avahi-0.1.2.tar.gz && cd napi-avahi-0.1.2 && sh install.sh
```

В образе должен быть `avahi-daemon` (`avahi-utils` желателен: без него новое имя применяется
перезапуском avahi и не работает `napi-avahi list`). Существующий `/etc/napi/avahi.conf`
сохраняется. Удаление: `sh uninstall.sh` (`--purge` удаляет и настройки).

## Команды на плате

```sh
napi-avahi show                 # что объявляет эта плата
napi-avahi role donoff,mgate    # роль (пусто: без роли)
napi-avahi name kotelnaya       # своё имя вместо napi<SoC>-<id> (пусто: вернуть)
napi-avahi list                 # все NAPI в сети
```

Настройки лежат в `/etc/napi/avahi.conf` (`ROLE=`, `NAME=`); после ручной правки `napi-avahi update`.

## Поиск с компьютера: napi-discover

Готовые программы: [Releases](https://github.com/lab240/napi-avahi/releases/latest):
`napi-discover.exe` (Windows 10/11), `napi-discover-linux-amd64` / `-arm64` (Linux; после
скачивания `chmod +x`). Один файл, ничего ставить не нужно.

- **Windows:** двойной щелчок открывает окно. **Linux:** запуск открывает страницу на 127.0.0.1
  в браузере (отдельным окном, если есть Chrome/Chromium; без Qt/GTK); программа выходит, когда
  страницу закрыли. Без графики (ssh) печатает таблицу, страницу и там откроет `--web`.
- «Искать»: платы с napi-avahi (зелёные строки). «Сырой поиск»: все адреса своей подсети с
  открытым портом 22, кроме шлюза и DNS, с признаками NAPI, для плат без napi-avahi. Вердикт:
  `NAPI` (есть napi-avahi), `NAPI?` (Linux и признаки: MAC как у Armbian, «napi» в имени,
  napiconfig на 8080, порты 8090/8086/3000/1883; жёлтые строки), `Linux`, `не NAPI` (MikroTik, Windows).
- napiconfig (8080), influxdb (8086), grafana (3000), donoff (8090) открывают выбранную плату в
  браузере. «SSH» и двойной щелчок по строке: терминал, спрашивает логин, `ssh <логин>@<IP>`
  (ключ хоста не проверяется: платы перепрошивают). Ctrl+C копирует IP.
- «Консоль ▾»: COM-порты (`/dev/ttyUSB*`, `/dev/ttyACM*`) с названием переходника и скорость
  (1500000, отладочный UART RK3308/RK3568, или 115200); консоль платы 8N1, выход Ctrl+].
  В Linux пользователь должен быть в группе `dialout`.
- «Журнал»: что делал последний поиск. «Help»: ссылки, лицензия.
- Командная строка: `napi-discover --console` (`-v` журнал, `-t 5s` слушать дольше, `--raw` сырой
  поиск), `napi-discover --serial COM7` или `--serial /dev/ttyUSB0` (`--baud 115200`).
- Windows может спросить про брандмауэр: разрешите для частной сети.
- Без программ: `avahi-browse -rt _napi._tcp` (Linux), по имени: `ping -4 napi3308-79df.local`.

Утилиты спрашивают mDNS с обычного UDP-порта (legacy unicast), поэтому не мешают
mDNS-службе Windows и работают без установки Bonjour. Кроме общего запроса (multicast) они
спрашивают напрямую каждый адрес своей подсети: многие точки доступа Wi-Fi не пропускают
multicast из проводной сети к своим клиентам, и NAPI на Wi-Fi общий запрос не слышит.

## Сборка

```sh
make test        # тесты скрипта на поддельной плате
make deb         # build/napi-avahi_<версия>_all.deb
make tools       # build/napi-discover.exe, napi-discover-linux-{amd64,arm64}
```

Пакет для repo.napilab.ru собирает `build-napi-avahi.sh` из
[lab240/repo-napilab](https://github.com/lab240/repo-napilab).

## Лицензия

MIT, © 2026 napilab (lab240), см. [LICENSE](LICENSE). Программа предоставляется «как есть», без
гарантий; авторы не отвечают за последствия её использования.
