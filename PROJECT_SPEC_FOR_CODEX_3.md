# PROJECT_SPEC_FOR_CODEX_3

## 1. Назначение проекта
Проект — CLI-утилита для пакетной переработки видеороликов из папки `input` в папку `output`.
Главная идея: не GUI-редактор, а управляемый пайплайн, который принимает набор видеофайлов и параметры запуска, после чего для каждого видео создает новую обработанную версию.

Проект не занимается:
- автоматической публикацией в TikTok;
- управлением аккаунтами;
- браузерной автоматизацией;
- обходом ограничений платформ.

Задача проекта — локальная обработка собственных видеофайлов.
Рабочее имя проекта: `videobatch`.

---

## 2. Архитектура и роли компонентов
Основная архитектурная идея:
- **Go** → CLI, orchestration, очередь задач, worker pool, recipe, запуск внешних инструментов.
- **FFmpeg** → основная видео- и аудиообработка.
- **ffprobe** → анализ исходного видео.
- **ExifTool** → чистка и диверсификация метаданных.
- **Python** → вспомогательные задачи, где это удобнее (например, анализ для smart pixel area), но не ядро приложения.

Go — главный процесс и orchestrator.

---

## 3. Принцип обязательности функций (обновлено)
Ключевое правило:
- Функции, признанные обязательными, **всегда включены по умолчанию**.
- Для обязательных функций **не добавляются bool-флаги включения/выключения**.
- Допускаются только параметрические флаги (диапазоны, интенсивность, режимы и т.п.), либо внутренние дефолты.

### 3.1 Обязательные функции
Обязательными считаются:
1. Startup Check зависимостей перед запуском пайплайна.
2. Генерация per-file recipe для каждого обработанного ролика.
3. Пер-файловая логика `tmp` cleanup:
   - после успешного рендера временные файлы удаляются;
   - при падении пайплайна временные файлы сохраняются для отладки.
4. Metadata-обработка в полном режиме: **clean + diversify** (не только чистка).
5. Color-трансформация.
6. Stream overlay.
7. Работа со скоростью **аудио**: Python-based WAV speed processor (base offset + slow sine + micro perturbations + short freeze repeats).
8. Работа со скоростью **видео**: Go-planned piecewise speed segments rendered через FFmpeg `trim` + `setpts` + `concat`.
9. Микро-события freeze: заморозка на **1 кадр**.
10. Микро-события replace: замена на **1 кадр** из другого stream source.
11. Pixel replacement (neighbor-duplication): микро-замены пикселей на каждом кадре в выбранной области.

### 3.2 Опциональные функции
Опциональными могут оставаться отдельные блоки (например trim/crop/captions/music), если это не противоречит текущему roadmap.

---

## 4. Startup Check (обязательный)
Перед началом обработки Go обязан проверить наличие:
- `ffmpeg`
- `ffprobe`
- `exiftool`
- `python3`

Если чего-то нет → понятная ошибка с инструкцией по установке, пайплайн не стартует.

Проверка Python-пакетов может быть:
- либо полной (единый обязательный список),
- либо условной по фактически используемым подпроцессам,
но итоговый runtime не должен стартовать без нужных библиотек для обязательных этапов.

---

## 5. Входные/выходные данные
### Вход
`input/` с видеофайлами.

Поддерживаемые форматы на старте:
- `.mp4`, `.mov`, `.mkv`
- `.webm` можно убрать из поддержки как необязательный формат.

### Выход
`output/` с файлами вида `*_processed.mp4`.

### Временные файлы
`tmp/` — промежуточные артефакты текущего ролика.

### Логи и рецепты
`logs/` — per-file recipe и логи.

---

## 6. Требования к temporal-изменениям (обязательный блок)

## 6.1 Audio speed: Python WAV processor
Audio speed handling реализуется отдельным Python-скриптом, который принимает WAV, сохраняет sample rate/channel layout и пишет WAV.
Основная speed-обработка аудио **не должна** опираться на FFmpeg `atempo`. FFmpeg может использоваться только для extraction/mux вокруг Python WAV processor.

Итоговая audio speed curve является композицией:
1. **Base speed offset** — малое статичное ускорение/замедление из recipe.
2. **Slow sine modulation** — очень малая амплитуда, длинный период, deterministic phase, perceptually invisible.
3. **Micro speed perturbations** — duration-dependent n-events; каждое событие имеет короткую длительность, малую `±delta`, seed-deterministic placement и smooth ramps.
4. **Audio freezing logic** — duration-dependent n-events; очень короткий segment (~10–40 ms) повторяется 2–4 раза и crossfade-сглаживается.

Требования:
- все transitions сглаживаются;
- speed clamp остается в safe bounds;
- не допускаются audible clicks/artifacts;
- итоговая длительность контролируется и подгоняется к planned video duration для sync safety.

## 6.2 Video speed: Go piecewise plan + FFmpeg render
Video speed handling остается внутри Go + FFmpeg, но **не** использует continuous analytic formula внутри FFmpeg.

Go строит piecewise speed plan из segments `{start, end, speed}`:
- `speed` включает base offset, discretized sine и micro jumps;
- sine аппроксимируется short segments (~200–400 ms);
- micro jumps являются short segments (~50–200 ms) с малым `±delta`;
- количество micro jumps зависит от длительности видео;
- planning полностью deterministic через seed/recipe;
- соблюдаются spacing constraints между событиями.

FFmpeg render:
- каждый segment режется через `trim`;
- для segment применяется `setpts=PTS/speed` (с reset через `PTS-STARTPTS`);
- все segments соединяются через `concat`;
- audio в этом video filtergraph не трогается.

Final video duration должна совпадать с planned duration. Processed audio подгоняется к этой planned duration, чтобы результат был sync-safe.

## 6.3 Base speed offset и sine mode
Для каждого ролика случайно выбирается знак base speed offset (ускорение или замедление) и процент изменения в заданном диапазоне.

Sine mode:
- `lock` — sine params audio/video синхронизированы;
- `independent` — sine params audio/video выбираются отдельно.

## 6.4 Freeze events (1-frame video)
- Video freeze должен быть визуально почти неразличим человеком.
- Длительность каждого video freeze-события: **ровно 1 кадр**.
- Количество freeze-событий зависит от длительности ролика.
- Моменты выбираются случайно.
- Обязательна проверка минимальной дистанции между temporal-событиями.

## 6.5 Replace events (1-frame video)
- Replace-событие — это замена ровно **1 кадра**.
- Кадр берется из **другого** stream source (не из того же фонового сегмента).
- Донорские кадры sourced из video assets, найденных через `--stream-overlay-dir`, используя ту же рекурсивную discovery-логику, что и stream overlay.
- Для каждого ролика выбирается один overlay stream и один donor stream; donor stream обязан отличаться от overlay stream и от main input video.
- Количество replace-событий зависит от длительности ролика.
- Моменты выбираются случайно.
- Обязательна проверка минимальной дистанции между temporal-событиями.
- Каждый replace обязан использовать **уникальный донорский кадр** (без повторов в рамках одного ролика).
- Replace реализуется runtime через piecewise temporal segmentation: основной видеопоток режется на сегменты до target frame, ровно один donor frame, затем сегменты после target frame; сегменты соединяются через `concat`.
- Replace не должен использовать `overlay enable` expressions; замена должна сохранять fps и непрерывность timeline.
- Все donor decisions (donor path, donor frame index, temp PNG path/effective count) фиксируются в recipe на planning stage; render только извлекает и потребляет эти инструкции.

---

## 7. Gaussian blur + Pixel replacement (обязательный блок)
Перед pixel replacement обязательно применяется очень слабое гауссово размытие (Gaussian blur)
на уровне, который практически не заметен человеческому глазу.

Для каждого кадра затем выполняется микро-модификация:
- случайный процент пикселей в заданном диапазоне заменяется на соседние (neighbor duplication);
- позиции пикселей выбираются случайно;
- обработка применяется не ко всей картинке, а к выбранной области.

### 7.0 Gaussian blur (weak)
- Blur является обязательной частью этого блока и применяется на слабом уровне.
- Интенсивность blur должна настраиваться параметрами диапазона (min/max) с рандомизацией в пределах диапазона.
- Цель blur: мягко замаскировать микро-артефакты pixel replacement без визуального эффекта "мыла".

### 7.1 Режимы выбора области
Должно быть 2 режима:
1. **Edge mode** — использовать зоны по краям кадра.
2. **Smart mode** — анализ видео по промежуткам и выбор области, где:
   - изменения между кадрами минимальны;
   - фон относительно однороден;
   - вмешательство наименее заметно для человеческого глаза.

---

## 8. Randomization constraints
Для всех рандомных событий и параметров:
- воспроизводимость через `seed` (если задан);
- ограничение минимального расстояния между temporal-событиями;
- валидация диапазонов параметров (проценты, counts, min/max).

---

## 9. Требования к recipe (обновлено)
Recipe должен быть доработан относительно предыдущей версии и покрывать:
1. Полную структуру обязательных блоков.
2. Temporal-параметры:
   - base speed для audio/video,
   - sine-параметры,
   - `av_sine_mode: lock|independent`,
   - audio/video speed micro perturbations,
   - audio freeze-repeat events,
   - video freeze events,
   - replace events,
   - ограничения по минимальным дистанциям.
3. Pixel replacement блок:
   - диапазоны процента замен,
   - mode (`edge`/`smart`),
   - параметры area selection,
   - параметры neighbor offset.
4. Метаданные в режиме full clean+diversify.

Важно: текущая реализация recipe из `PROJECT_SPEC_FOR_CODEX_2.md` считается недостаточной и должна быть расширена под эти требования.

---

## 10. Валидация и исправления логики

### 10.1 Crop validation
Если crop остается опциональным блоком:
- валидация `crop-*` параметров должна выполняться только при включенном crop.

### 10.2 Crop percent math
Использовать корректную интерпретацию процентов:
- `ratio = percent / 100.0`.

### 10.3 Crop position
Если используется только random-area логика, флаг `--crop-position` можно удалить как лишний.

### 10.4 Stream overlay scanning
Поиск overlay-файлов должен быть рекурсивным, включая подпапки.

### 10.5 ffprobe audio permissiveness
Видео без аудиодорожки должны считаться валидными для обработки (при наличии видео-потока).

---


## 11. Рекомендованный порядок применения эффектов
Обычно лучше применять эффекты в следующем порядке:
1. основная геометрия/цвет;
2. слабый Gaussian blur;
3. пиксельные микро-подмены (neighbor duplication);
4. temporal-эффекты (speed/freeze/replace);
5. overlay;
6. финальные операции кодирования/сборки.

---

## 12. Поведение worker pipeline
Даже если разработка ведется поэтапно (сначала флаги и recipe), целевая модель остается:
- scan input;
- probe source;
- generate recipe;
- render pipeline;
- metadata full mode;
- write logs/recipe;
- cleanup tmp по policy.

---

## 13. Обратная совместимость
- `PROJECT_SPEC_FOR_CODEX_2.md` остается в репозитории и не удаляется.
- Новый документ (`PROJECT_SPEC_FOR_CODEX_3.md`) является актуализированной спецификацией с учетом обсужденных правок.
