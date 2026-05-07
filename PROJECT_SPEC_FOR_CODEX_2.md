PROJECT_SPEC_FOR_CODEX

1. Назначение проекта
Проект — CLI-утилита для пакетной переработки видеороликов из папки `input` в папку `output`.
Главная идея: не GUI-редактор, а управляемый пайплайн, который принимает набор видеофайлов, набор флагов и конфигов, после чего для каждого видео создает новую обработанную версию.
Проект не должен заниматься автоматической публикацией в TikTok, управлением аккаунтами, браузерной автоматизацией или обходом ограничений платформ. Задача проекта — локальная обработка собственных видеофайлов.
Рабочее имя проекта: `videobatch`.

2. Общая архитектура
Основная архитектурная идея:
Go       -> CLI, очередь задач, многопоточность, recipe.json, запуск внешних инструментов
FFmpeg   -> основная видео- и аудиообработка
ffprobe  -> анализ исходного видео
ExifTool -> чтение, чистка и запись метаданных
Python   -> автотитры и сложная аудиокривая / audio envelope
Go является главным процессом. Python не должен быть ядром всего приложения. Python вызывается только для тех задач, где он заметно удобнее:
- автотранскрибация речи;
- генерация ASS/SRT-субтитров по шаблонам;
- генерация и применение сложной аудиокривой;
- возможно, вспомогательная аудиоаналитика.
FFmpeg выполняет тяжелую обработку видео и аудио. Go должен вызывать FFmpeg напрямую, а не через Python, кроме тех случаев, где Python заранее готовит отдельный обработанный audio track или subtitle file.

3. Основной сценарий работы
Пользователь запускает CLI примерно так:
videobatch \
  --input ./input \
  --output ./output \
  --jobs 4 \
  --threads-per-job 4 \
  --trim \
  --crop \
  --color-preset random \
  --color-strength soft \
  --captions auto \
  --caption-template random \
  --audio-envelope python \
  --music-dir ./assets/music \
  --music-ducking \
  --stream-overlay-dir ./assets/stream_overlays \
  --stream-overlay-opacity 0.02 \
  --stream-overlay-mode stealth \
  --metadata clean-and-diversify \
  --temporal-shift 0.003 \
  --codec-profile balanced

CLI должен позволять включать и отключать отдельные блоки обработки флагами, потому что не все видео требуют одинаковых действий.
Примеры ситуаций:
- на части видео уже есть титры;
- часть видео уже вручную обрезана;
- на части видео уже есть музыка;
- некоторые видео не требуют overlay-слоя;
- иногда нужна только чистка метаданных и цветовой пресет.

⚠️ Startup Check (запуск):
Перед началом обработки Go обязан проверить наличие `ffmpeg`, `ffprobe`, `exiftool`, `python3` и базовых библиотек Python. Если что-то отсутствует → понятная ошибка с инструкцией по установке, пайплайн не стартует.

4. Входные и выходные данные
Вход
Основной вход:
input/
  video_001.mp4
  video_002.mov
  video_003.mkv
Поддерживаемые форматы на старте:
`.mp4`, `.mov`, `.mkv` (`.webm` — опционально)

Выход
output/
  video_001_processed.mp4
  video_002_processed.mp4
  video_003_processed.mp4

Временные файлы
tmp/
  video_001_audio.wav
  video_001_processed_audio.wav
  video_001_captions.ass
  video_001_temporal.mp4 (если применялся shift)

Логи и рецепты
logs/
  video_001.recipe.json
  video_001.ffprobe.json
  video_001.metadata.before.json
  video_001.metadata.after.json
  video_001.log

⚠️ Очистка tmp/:
После успешного рендера и записи логов Go обязан удалить все временные файлы конкретного ролика. При падении пайплайна `tmp/` сохраняется для отладки. Флаг `--cleanup` (по умолчанию `true`).

Каждое обработанное видео должно иметь свой `recipe.json`, чтобы можно было понять, какие параметры были применены.

5. Предлагаемая структура проекта
videobatch/
  cmd/
    videobatch/
      main.go

  internal/
    config/
      config.go
      flags.go

    scanner/
      scanner.go

    workerpool/
      pool.go
      job.go

    ffprobe/
      ffprobe.go
      models.go

    ffmpeg/
      builder.go
      filters.go
      runner.go

    metadata/
      exiftool.go
      policy.go

    recipe/
      recipe.go
      generator.go
      random.go

    logging/
      logger.go

  python/
    transcribe.py
    build_captions_ass.py
    audio_envelope.py
    requirements.txt

  configs/
    color_presets/
    caption_templates/
    metadata_policy.json

  assets/
    music/
    stream_overlays/
      default/
      stealth/
    fonts/

  input/
  output/
  tmp/
  logs/

6. Разделение ответственности
Go
Go отвечает за:
CLI-флаги; загрузку конфигов; обход input-папки; фильтрацию файлов; создание очереди задач; worker pool; управление `--jobs` и `--threads-per-job`; вызов `ffprobe`, `exiftool`, Python-модулей; генерацию `recipe.json`; выбор случайных параметров; выбор музыки/overlay; сборку и запуск FFmpeg; запись логов; обработку ошибок; graceful shutdown.

Python
Автотранскрибация; генерация `.ass`/`.srt`; работа с шаблонами; генерация и применение audio envelope; чтение/запись WAV.

FFmpeg
trim; crop; scale; цветокоррекция; наложение субтитров и overlay; подмешивание музыки и ducking; temporal/codec shift; финальный mux и кодирование.

ffprobe
duration, width, height, fps, codecs, наличие аудио, rotation, sample rate, channel layout.

ExifTool
чтение/сохранение `metadata.before.json`; чистка метаданных; запись новых тегов/рандомизация; сохранение `metadata.after.json`; безопасная работа с атомами контейнера.

7. Recipe JSON
Go должен генерировать отдельный recipe для каждого видео.
Пример (валидный JSON, пробелы только внутри строк, ключи без пробелов):
{
  "input": "input/video_001.mp4",
  "output": "output/video_001_processed.mp4",
  "seed": 18492012,
  "source": {
    "duration": 27.4,
    "width": 1080,
    "height": 1920,
    "fps": 30,
    "has_audio": true,
    "rotation": 0
  },
  "trim": {
    "enabled": true,
    "start_sec": 0.62,
    "end_sec": 0.47
  },
  "crop": {
    "enabled": true,
    "zoom": 1.025,
    "x_offset_px": 4,
    "y_offset_px": -8
  },
  "color": {
    "enabled": true,
    "preset": "warm",
    "strength": "soft",
    "brightness": 0.012,
    "contrast": 1.034,
    "saturation": 1.061,
    "gamma": 0.992
  },
  "captions": {
    "enabled": true,
    "mode": "auto",
    "template": "meme_big_bottom",
    "ass_file": "tmp/video_001_captions.ass"
  },
  "audio_envelope": {
    "enabled": true,
    "mode": "python",
    "input_audio": "tmp/video_001_audio.wav",
    "output_audio": "tmp/video_001_processed_audio.wav",
    "base_gain": 0.991,
    "slow_sine": {
      "enabled": true,
      "amplitude": 0.012,
      "period_sec": 8.5,
      "phase": 1.4
    },
    "random_points": {
      "enabled": true,
      "count": 10,
      "min_gain_delta": -0.025,
      "max_gain_delta": 0.025
    },
    "random_dips": {
      "enabled": true,
      "count": 4,
      "depth_min": 0.02,
      "depth_max": 0.06,
      "duration_min_sec": 0.15,
      "duration_max_sec": 0.45
    },
    "smooth_ms": 120,
    "min_gain": 0.92,
    "max_gain": 1.06
  },
  "music": {
    "enabled": true,
    "file": "assets/music/calm/track_03.mp3",
    "volume": 0.06,
    "ducking": true,
    "duck_ratio": 6,
    "duck_attack_ms": 20,
    "duck_release_ms": 300
  },
  "temporal_codec": {
    "enabled": true,
    "speed_multiplier": 0.997,
    "fps_override": 30.01,
    "preset": "medium",
    "crf": 23,
    "x264_params": "bframes=3:b-adapt=2:rc-lookahead=40"
  },
  "stream_overlay": {
    "enabled": true,
    "mode": "stealth",
    "file": "assets/stream_overlays/stealth/noise_grid.mp4",
    "scale_mode": "cover",
    "opacity": 0.02,
    "noise_sigma": 0.005,
    "translate_px": [0.3, -0.5],
    "color_tweak": true,
    "loop": true,
    "include_audio": false
  },
  "metadata": {
    "enabled": true,
    "mode": "clean-and-diversify",
    "before_file": "logs/video_001.metadata.before.json",
    "after_file": "logs/video_001.metadata.after.json"
  }
}

8. CLI-флаги
Базовые
--input string
--output string
--tmp string
--logs string
--jobs int
--threads-per-job int
--seed int
--dry-run
--overwrite
--cleanup bool (default true)

Trim
--trim
--trim-start-min float
--trim-start-max float
--trim-end-min float
--trim-end-max float

Crop
--crop
--crop-min-percent float
--crop-max-percent float
--crop-position random|center|top|bottom

Color
--color-preset string
--color-strength soft|medium|hard
--color-config-dir string

Captions
--captions off|auto
--caption-template string
--caption-template-dir string
--caption-language string
--caption-model string

Audio envelope
--audio-envelope off|python
--audio-envelope-config string

Music
--music-dir string
--music-volume float
--music-ducking
--duck-ratio float
--duck-attack-ms int
--duck-release-ms int

Stream overlay
--stream-overlay-dir string
--stream-overlay-opacity float
--stream-overlay-mode normal|stealth
--stream-overlay-random

Metadata
--metadata off|read|clean|clean-and-diversify
--metadata-policy string

Temporal & Codec
--temporal-shift float
--fps-tweak float
--codec-profile fast|balanced|strong

9. Аудиокривая через Python
Текущий выбранный подход: делать audio envelope через Python.
Логика envelope:
1. base_gain = 0.98–1.02
2. slow_sine = мягкая синусоида
3. random_points = случайные контрольные точки
4. random_dips = редкие мягкие просадки
5. smoothing = сглаживание, чтобы не было щелчков
6. final_gain ограничивается 0.92–1.06

Python-модуль
Файл: python/audio_envelope.py
Ожидаемый CLI-интерфейс:
python3 python/audio_envelope.py \
  --input tmp/video_001_audio.wav \
  --output tmp/video_001_processed_audio.wav \
  --config logs/video_001.recipe.json

Библиотеки
numpy, scipy, soundfile. Опционально: librosa, pyloudnorm.

Требования к audio envelope
- Не должно быть резких скачков громкости.
- Все изменения должны быть сглажены (≥50ms переходы).
- Итоговый gain ограничен диапазоном.
- После обработки не должно быть клиппинга.
- Корректная работа с mono/stereo.
- Sample rate и длина сохраняются.
- Если входной файл без аудио → graceful skip, код 0, лог `skip: no audio track`.
- Все random-параметры сидируются через `--seed` из Go для воспроизводимости.

10. Фоновая музыка и ducking
Фоновая музыка берется из `assets/music/`.
Рекомендованный подход:
- выбрать случайный трек;
- подогнать длительность под ролик;
- зациклить, если трек короткий;
- выставить низкую базовую громкость;
- сделать fade-in/fade-out;
- применить ducking от оригинального звука;
- смешать с оригинальным processed audio.
Музыка не должна использовать такую же сложную envelope-кривую, как оригинальный звук. Она остаётся стабильной и управляется ducking-логикой.

10.1. Temporal & Codec Micro-Shift (опциональный модуль)
Назначение: сдвиг temporal fingerprint и структуры кодирования без видимых артефактов и рассинхрона.

Логика:
- Если `--temporal-shift` задан, FFmpeg применяет в одном `filter_complex`:
  `[0:v]setpts=PTS*(1+shift)[v]; [0:a]atempo=1/(1+shift)[a]`
  Длительность меняется пропорционально, аудио/видео остаются синхронными.
- Если `--fps-tweak` задан, добавляется `-r <value> -fps_mode vfr` перед финальным mux.
- `--codec-profile` выбирает preset/CRF/x264-params из безопасного диапазона. Качество визуально одинаковое, но байтовый fingerprint и GOP-структура уникальны.

⚠️ Ограничения и обход рисков:
- Не применять к роликам `< 3.0s` (риск артефактов и рассинхрона).
- `setpts` и `atempo` должны вызываться вместе в одном пайплайне FFmpeg. Раздельный вызов запрещён.
- Перед запуском проверять наличие фильтров `setpts`/`atempo` через `ffmpeg -filters`. Если отсутствуют → шаг `skipped` с warning.
- Dry-run обязан вычислять итоговую длительность и предупреждать, если shift > 0.5%.
- Параметры генерируются в Go, сидируются через `--seed`, записываются в `recipe.json`.

Пример конфигурации в recipe:
{
  "temporal_shift": {
    "enabled": true,
    "speed_multiplier": 0.997,
    "fps_override": 30.01
  },
  "codec_variation": {
    "preset": "medium",
    "crf": 23,
    "x264_params": "bframes=3:b-adapt=2:rc-lookahead=40"
  }
}

11. Автотитры
Автотитры должны работать через Python.
Рекомендуемый стек:
faster-whisper -> распознавание речи
pysubs2        -> генерация ASS/SRT
FFmpeg         -> прожигание ASS в видео
Пайплайн:
video.mp4
  -> FFmpeg extracts audio.wav
  -> faster-whisper transcribes speech
  -> Python groups words/segments
  -> pysubs2 builds .ass
  -> FFmpeg burns subtitles into output video
Нужно поддержать шаблоны титров.

12. ASS/SRT-шаблоны
Шаблоны титров должны лежать в `configs/caption_templates/`.
ASS предпочтительнее SRT для финального видео, потому что позволяет задавать стиль, размер, шрифт, позицию, обводку, тень, цвета и разные шаблоны отображения.
SRT можно использовать как промежуточный формат.

13. Цветовые пресеты
Цветовые пресеты должны быть семействами диапазонов, а не фиксированными значениями.
Нужно поддержать strength:
soft   -> почти незаметно
medium -> заметно, но не агрессивно
hard   -> стилизованно
Обработка через FFmpeg-фильтры (`eq`, `curves`, `hue`).

14. Stream overlay
Текущий подход: второй видеослой из пользовательских материалов.
Поддерживаемые режимы:
- normal: обычное наложение с видимой прозрачностью
- stealth: наложение с минимальной видимостью, направленное на изменение перцептивного хэша

Требования к stealth-режиму:
- opacity 0.01–0.03
- опциональный high-frequency noise/grain слой (σ ≈ 0.005)
- субпиксельное смещение (translate=0.3,-0.5)
- легкий сдвиг цветовой матрицы (hue=s:0.98 или colorspace tweak)
- overlay берётся из отдельной папки, масштабируется под основное видео (scale_mode=cover)
- если overlay короче → зацикливание
- audio overlay всегда отключён
- параметры настраиваются через флаги, чтобы не ломать normal-режим

FFmpeg-реализация (концептуально):
`[main][overlay]overlay=x=0:y=0:alpha=0.02,format=yuv420p,colorspace=bt709:iall=bt601:range=pc[out]`

15. Метаданные через ExifTool
Нужен отдельный metadata policy.
Файл: `configs/metadata_policy.json`

Пример профиля `clean-and-diversify`:
{
  "mode": "clean-and-diversify",
  "remove_all": true,
  "preserve": ["Rotation", "ColorProfile"],
  "randomize": {
    "dates": true,
    "range_days": [-180, 365]
  },
  "inject": {
    "Software": ["videobatch", "ffmpeg", "HandBrake", "DaVinci Resolve", "Adobe Premiere"],
    "Comment": ["exported", "remastered", "batch v2", "optimized"],
    "CustomUUID": "generate_v4"
  },
  "atom_tweaks": {
    "shuffle_moov_order": true
  }
}

Поведение:
- До обработки сохранить метаданные исходника в `logs/*.metadata.before.json`
- После FFmpeg-рендера применить ExifTool к результату по профилю
- При `remove_all=true` удаляются все теги, кроме `preserve`
- При `randomize.dates=true` даты смещаются в заданном диапазоне
- При `inject` случайным образом выбирается значение из массива или генерируется UUID
- При `atom_tweaks.shuffle_moov_order=true` порядок атомов меняется через `ffmpeg -movflags +randomize` (без перекодирования)
- Сохранить финальные метаданные в `logs/*.metadata.after.json`
- ⚠️ Не сломать orientation/rotation. Если FFmpeg уже нормализовал поворот, это фиксируется в recipe/logs
- ⚠️ Если `exiftool` недоступен → шаг `skipped` с warning, пайплайн не падает

16. Обработка ошибок
Пайплайн не должен падать полностью из-за одного проблемного файла.
Статусы: pending, processing, success, failed, skipped.
Ошибки пишутся в отдельный лог.
Типичные ошибки: FFmpeg/ffprobe/ExifTool/Python не найдены; нет аудио; файл не читается; видео слишком короткое; нет музыки/overlay; неверный JSON; ошибка кодирования.
Поведение при отсутствии модулей:
- нет аудио -> пропустить audio envelope и captions;
- нет music-dir -> пропустить музыку или вернуть ошибку в strict-режиме;
- нет overlay-dir -> пропустить overlay;
- видео слишком короткое -> уменьшить trim или отключить.
⚠️ Логи: рекомендован структурированный JSON-лог (уровень, timestamp, step, message, error) для удобного парсинга.
⚠️ Потоки/CPU: каждый FFmpeg-процесс получает `max(1, threads_per_job / jobs)` через `-threads`. Python ограничивается `OMP_NUM_THREADS`. Go проверяет `runtime.NumCPU()` перед стартом воркеров.

17. Dry run
Нужен `--dry-run`.
В dry-run режиме:
- просканировать input;
- получить ffprobe info;
- сгенерировать recipe;
- показать/сохранить план обработки;
- не запускать финальный FFmpeg render;
- не перезаписывать output;
- вычислить итоговую длительность при temporal-shift и вывести предупреждение, если >0.5%.

18. Детерминированность
Нужен `--seed`.
Если seed задан, один и тот же вход и конфиг должны давать один и тот же recipe.
Это важно для отладки и воспроизводимости batch-обработки.
Все `random`-выборы (preset, overlay, music, envelope points, metadata dates, codec params) инициализируются через seed.

19. Что не делать
Не реализовывать:
- автоматическую загрузку видео на платформы;
- управление аккаунтами / авторизацию / API-публикацию;
- браузерную автоматизацию / эмуляцию пользователей;
- GUI на первом этапе;

Разрешено и документировано:
- изменение визуальных fingerprint через stealth-overlay и микро-сдвиги;
- диверсификация метаданных (EXIF/QuickTime/XMP) для снижения совпадений по точным хэшам;
- все изменения должны оставаться в рамках локальной batch-обработки и не требовать сетевых запросов к платформам.

20. Ключевые решения, уже принятые
- Основной язык CLI — Go.
- Главный рендер-инструмент — FFmpeg.
- Технический анализ — ffprobe.
- Метаданные — ExifTool (`clean-and-diversify`).
- Автотитры — Python + faster-whisper + pysubs2.
- ASS-шаблоны используются для финальных титров.
- Аудиокривая — Python (base_gain + sine + points + dips + smoothing + clamp).
- Фоновая музыка — ducking, стабильная кривая.
- Цветовые пресеты — семейства диапазонов.
- Stream overlay — `normal`/`stealth` режимы, микро-прозрачность, субпиксельный сдвиг.
- Temporal & Codec — опциональный модуль `setpts+atempo` + `fps_tweak` + `codec_variation` с защитой от рассинхрона и коротких роликов.
- Dependency check, structured logging, tmp cleanup, thread/CPU limits реализованы на старте.
- Для каждого файла сохраняется `recipe.json` (валидный JSON).
- CLI позволяет отключать/включать каждый модуль отдельно.
- Код не писать внутри этого описания; файл является ориентиром для Codex.
