#!/usr/bin/env python3
"""Same-machine benchmark: noah vs Engram, through their CLIs.

Usage: BENCH_DIR=/tmp/noah-bench bench.py <noah|engram|engram-any> [N_MAX]
Build first: go build -o $BENCH_DIR/noah ./cmd/noah
Corpus, needles, distractors and questions are generated from a fixed seed, so
every tool sees identical data. Nothing here touches ~/.noah or ~/.engram.
"""
import json, os, random, re, statistics, subprocess, sys, time

TOOL = sys.argv[1]
NMAX = int(sys.argv[2]) if len(sys.argv) > 2 else 10000
CHECKPOINTS = [c for c in (1000, 10000) if c <= NMAX]
WORK = os.environ.get("BENCH_DIR", "/tmp/noah-bench")  # scratch dir; binaries go in WORK/noah and WORK/engram
RUN = f"{WORK}/run/{TOOL}"
CWD = f"{WORK}/proj"
os.makedirs(RUN, exist_ok=True)
os.makedirs(CWD, exist_ok=True)

ENV = dict(os.environ)
ENV["HOME"] = RUN + "/home"
os.makedirs(ENV["HOME"], exist_ok=True)
if TOOL == "noah":
    BIN = f"{WORK}/noah"
    DATA = RUN + "/noah.db"
    ENV["NOAH_DB"] = DATA
    ENV["NOAH_SPACE"] = "bench"
    DATA_FILES = lambda: [DATA, DATA + "-wal", DATA + "-shm"]
else:
    BIN = f"{WORK}/engram"
    ddir = RUN + "/data"
    os.makedirs(ddir, exist_ok=True)
    ENV["ENGRAM_DATA_DIR"] = ddir
    DATA_FILES = lambda: [os.path.join(ddir, f) for f in os.listdir(ddir)]


def run(args):
    t = time.perf_counter()
    p = subprocess.run([BIN] + args, cwd=CWD, env=ENV, capture_output=True, text=True)
    return (time.perf_counter() - t) * 1000, p.returncode, p.stdout + p.stderr


def save(title, content, kind=None):
    if TOOL == "noah":
        a = ["save", "--title", title]
        if kind:
            a += ["--kind", kind]
        return run(a + [content])
    a = ["save", title, content, "--project", "bench"]
    if kind:
        a += ["--type", kind]
    return run(a)


def search(q, any_mode=False):
    if TOOL == "noah":
        return run(["search", q])
    a = ["search", q, "--project", "bench"]
    if any_mode:
        a += ["--match", "any"]
    return run(a)


# ---------------- deterministic corpus ----------------
rng = random.Random(42)
COMMON = ("servicio datos equipo usuario despliegue configuración error cliente token cache cola test release rama "
          "módulo producción staging logs métrica alerta límite pool conexiones versión migración índice consulta "
          "respuesta permisos rol correo pago factura pedido informe service database deploy config queue retry "
          "timeout cluster worker schema index backend frontend pipeline build commit review ticket sprint "
          "nodo clave sesión proyecto repositorio contenedor imagen puerto dominio certificado copia restauración").split()
SYL = "ka ro mi tu ve lo sa ne di po xu qe zi bo fa gu he jo ly mo".split()


def pseudo(used):
    while True:
        w = "".join(rng.choice(SYL) for _ in range(rng.randint(3, 4)))
        if w not in used:
            used.add(w)
            return w


used = set(COMMON)
RARE = [pseudo(used) for _ in range(4000)]
ENTITIES = [pseudo(used).capitalize() for _ in range(200)]
zipf_c = [1 / (i + 1) for i in range(len(COMMON))]
zipf_r = [1 / (i + 1) ** 0.9 for i in range(len(RARE))]


def filler_text(n_words=130):
    w = []
    for _ in range(n_words):
        if rng.random() < 0.55:
            w.append(rng.choices(COMMON, zipf_c)[0])
        else:
            w.append(rng.choices(RARE, zipf_r)[0])
    s = " ".join(w)
    return (s[:1].upper() + s[1:] + ".")[:900]


MID = COMMON[12:70]  # topics: not the most frequent words, still common in filler
rows = []  # (kind_code, title, content, type)
needles = []
for i in range(40):
    t1, t2 = rng.sample(MID, 2)
    ent = ENTITIES[i]
    d1, d2 = rng.sample(RARE[1500:], 2)
    code = f"Needle-{i:03d}"
    content = f"Decidimos {t1} y {t2} para {ent}: {d1} {d2}. " + filler_text(60)
    needles.append(dict(i=i, code=code, t1=t1, t2=t2, ent=ent, d1=d1, d2=d2))
    rows.append((code, f"{code} {t1} {t2} {ent}", content, "decision"))
    # hard negatives: share two of the question terms, not the right one
    rows.append(("Dist", f"Distractor-{i:03d}a {t1} {t2} {ENTITIES[100 + i]}",
                 f"Decidimos {t1} y {t2} para {ENTITIES[100 + i]}: {rng.choice(RARE[1500:])}. " + filler_text(60), "decision"))
    rows.append(("Dist", f"Distractor-{i:03d}b {rng.choice(MID)} {ent}",
                 f"Notas sobre {ent} y {rng.choice(MID)}: {rng.choice(RARE[1500:])}. " + filler_text(60), "decision"))
    rows.append(("Dist", f"Distractor-{i:03d}c {t1} {rng.choice(MID)}",
                 f"Sobre {t1}: {rng.choice(RARE[1500:])}. " + filler_text(60), "decision"))
# pad first 1000 rows with filler, shuffle them, then more filler up to NMAX
head = rows[:]
while len(head) < 1000:
    head.append(("Note", f"Note-{len(head):05d} " + " ".join(rng.choices(COMMON, k=3)) + " " + rng.choice(RARE),
                 filler_text(), None))
rng.shuffle(head)
corpus = head[:]
while len(corpus) < NMAX:
    corpus.append(("Note", f"Note-{len(corpus):05d} " + " ".join(rng.choices(COMMON, k=3)) + " " + rng.choice(RARE),
                   filler_text(), None))

questions = []
for n in needles[:20]:
    questions.append(("full", n["i"], f"¿Qué decidimos sobre {n['t1']} {n['t2']} de {n['ent']} con {n['d1']}?"))
for n in needles[20:]:
    questions.append(("partial", n["i"], f"cómo configuramos {n['ent']} para {n['t1']} en producción"))

HIT = re.compile(r"Needle-(\d+)")


def top_codes(out):
    seen = []
    for m in HIT.finditer(out):
        v = int(m.group(1))
        if v not in seen:
            seen.append(v)
    return seen


def pct(vals, p):
    vals = sorted(vals)
    return vals[min(len(vals) - 1, int(round(p / 100 * (len(vals) - 1))))]


def size_mb():
    return sum(os.path.getsize(f) for f in DATA_FILES() if os.path.isfile(f)) / 1e6


def recall(any_mode):
    lat = []
    stats = {"full": [0, 0, 0], "partial": [0, 0, 0]}
    empty = {"full": 0, "partial": 0}
    for kind, idx, q in questions:
        ms, rc, out = search(q, any_mode)
        lat.append(ms)
        codes = top_codes(out)
        stats[kind][2] += 1
        if codes and codes[0] == idx:
            stats[kind][0] += 1
        if idx in codes[:5]:
            stats[kind][1] += 1
        if not re.search(r"\[\d+\] #\d+|^#\d+ \[", out, re.M):
            empty[kind] += 1
    rec = {k: {"hit@1": v[0], "hit@5": v[1], "of": v[2], "no_results": empty[k]} for k, v in stats.items()}
    return rec, round(statistics.median(lat), 1), round(pct(lat, 95), 1)


def checkpoint(n, save_times):
    res = {"n": n, "size_mb": round(size_mb(), 1),
           "save_p50_ms": round(statistics.median(save_times), 1), "save_p95_ms": round(pct(save_times, 95), 1)}
    res["recall"], res["nl_search_p50_ms"], res["nl_search_p95_ms"] = recall(False)
    if TOOL == "engram":
        res["recall_match_any"], res["nl_any_p50_ms"], res["nl_any_p95_ms"] = recall(True)
    # keyword latencies
    for name, q in (("common_term", "datos"), ("three_terms", "cola retry timeout")):
        ts = [search(q)[0] for _ in range(30)]
        res[name + "_p50_ms"] = round(statistics.median(ts), 1)
        res[name + "_p95_ms"] = round(pct(ts, 95), 1)
    return res


results = []
save_times = []
t0 = time.time()
for i, (code, title, content, kind) in enumerate(corpus, 1):
    ms, rc, out = save(title, content, kind)
    if rc != 0:
        print("SAVE FAILED", i, title, out[:300], flush=True)
        sys.exit(2)
    save_times.append(ms)
    if i % 500 == 0:
        print(f"{TOOL}: {i}/{NMAX} saved, {time.time() - t0:.0f}s", flush=True)
    if i in CHECKPOINTS:
        r = checkpoint(i, save_times[-100:])
        results.append(r)
        print(json.dumps(r, ensure_ascii=False), flush=True)
        json.dump(results, open(f"{WORK}/results_{TOOL}.json", "w"), ensure_ascii=False, indent=1)
print("DONE", TOOL, f"{time.time() - t0:.0f}s", flush=True)
