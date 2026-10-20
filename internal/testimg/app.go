package testimg

import (
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// body returns n bytes of c, so different files hash differently.
func body(c byte, n int64) string { return strings.Repeat(string(c), int(n)) }

// AppSteps is a small Python app image with every kind of change: a base
// layer, an apt install that leaves its lists behind and replaces a
// library, a pip install that deletes those lists (freeing nothing), a COPY
// that brings a .env file and a big model, and a chown -R that stores the
// same files again. Sizes are multiplied by unit.
func AppSteps(unit int64) []Step {
	return []Step{
		Layer("/bin/sh -c #(nop) ADD file:4f2a in / ",
			Dir("etc"),
			File{Path: "etc/os-release", Body: body('o', 2*unit)},
			Dir("usr"), Dir("usr/bin"), Dir("usr/lib"),
			File{Path: "usr/bin/bash", Body: body('b', 120*unit), Mode: 0o755},
			File{Path: "usr/lib/libc.so.6", Body: body('c', 200*unit)},
			File{Path: "usr/bin/sh", Type: '2', Link: "bash"},
		),
		Empty("/bin/sh -c #(nop)  ENV PYTHONUNBUFFERED=1"),
		Layer("RUN |1 DEBIAN_FRONTEND=noninteractive /bin/sh -c apt-get update && apt-get install -y curl # buildkit",
			File{Path: "usr/bin/curl", Body: body('u', 30*unit), Mode: 0o755},
			File{Path: "usr/lib/libc.so.6", Body: body('C', 210*unit)},
			Dir("var"), Dir("var/lib"), Dir("var/lib/apt"), Dir("var/lib/apt/lists"),
			File{Path: "var/lib/apt/lists/deb_main_Packages", Body: body('p', 50*unit)},
			File{Path: "var/lib/apt/lists/deb_updates_Packages", Body: body('q', 40*unit)},
		),
		Layer("WORKDIR /app", Dir("app")),
		Layer("RUN /bin/sh -c pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*",
			File{Path: "usr/local/lib/python3.12/site-packages/pandas/core.py", Body: body('1', 300*unit)},
			File{Path: "usr/local/lib/python3.12/site-packages/pandas/io.py", Body: body('2', 100*unit)},
			File{Path: "usr/local/lib/python3.12/site-packages/numpy/core.so", Body: body('3', 250*unit)},
			File{Path: "usr/local/lib/python3.12/site-packages/six.py", Body: body('4', 1*unit)},
			Whiteout("var/lib/apt/lists/deb_main_Packages"),
			Whiteout("var/lib/apt/lists/deb_updates_Packages"),
		),
		Layer("COPY . . # buildkit",
			File{Path: "app/main.py", Body: body('m', 2*unit)},
			File{Path: "app/.env", Body: "SECRET=1"},
			File{Path: "app/models/base.onnx", Body: body('x', 600*unit)},
		),
		Layer("RUN /bin/sh -c chown -R 1000:1000 /app",
			File{Path: "app/main.py", Body: body('m', 2*unit), UID: 1000, GID: 1000},
			File{Path: "app/.env", Body: "SECRET=1", UID: 1000, GID: 1000},
			File{Path: "app/models/base.onnx", Body: body('x', 600*unit), UID: 1000, GID: 1000},
		),
		Empty(`/bin/sh -c #(nop)  CMD ["python" "main.py"]`),
	}
}

// App builds the AppSteps image.
func App(unit int64) v1.Image { return Build(AppSteps(unit)...) }

// AppNext is the next release of App: pandas grew, numpy is gone, a module
// was added and main.py changed; the base, apt and WORKDIR layers are the
// same.
func AppNext(unit int64) v1.Image {
	steps := AppSteps(unit)
	steps[4] = Layer("RUN /bin/sh -c pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*",
		File{Path: "usr/local/lib/python3.12/site-packages/pandas/core.py", Body: body('5', 420*unit)},
		File{Path: "usr/local/lib/python3.12/site-packages/pandas/io.py", Body: body('2', 100*unit)},
		File{Path: "usr/local/lib/python3.12/site-packages/six.py", Body: body('4', 1*unit)},
		Whiteout("var/lib/apt/lists/deb_main_Packages"),
		Whiteout("var/lib/apt/lists/deb_updates_Packages"),
	)
	steps[5] = Layer("COPY . . # buildkit",
		File{Path: "app/main.py", Body: body('n', 3*unit)},
		File{Path: "app/api/retry.py", Body: body('r', 4*unit)},
		File{Path: "app/models/base.onnx", Body: body('x', 600*unit)},
	)
	steps[6] = Layer("RUN /bin/sh -c chown -R 1000:1000 /app",
		File{Path: "app/main.py", Body: body('n', 3*unit), UID: 1000, GID: 1000},
		File{Path: "app/api/retry.py", Body: body('r', 4*unit), UID: 1000, GID: 1000},
		File{Path: "app/models/base.onnx", Body: body('x', 600*unit), UID: 1000, GID: 1000},
	)
	return Build(steps...)
}
