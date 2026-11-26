# Third-party notices

Kubetail's Linux packages carry the third-party programs below, unmodified. Kubetail's own license
does not cover them; each is under its own license, whose text sits beside this file.

Each Kubetail desktop release on GitHub attaches `kubetail-desktop-<version>-embedded-sources.tar.gz`,
the complete source its bundled `bwrap` and `pasta` were built from: each upstream tarball
unpacked unchanged, with the scripts that built it. It is under the release's `desktop/v<version>` tag at
<https://github.com/kubetail-org/kubetail/releases>, and `SHA256SUMS` covers it.

## Bubblewrap

- **Bundled as:** `bwrap` (`/usr/lib/kubetail-desktop/bwrap`), in the `.deb` and the `.rpm`
- **Version:** 0.13.1
- **Upstream:** <https://github.com/containers/bubblewrap>
- **Source:** `bubblewrap/` in the release's embedded-sources archive, unpacked from
  <https://github.com/containers/bubblewrap/releases/download/v0.13.1/bubblewrap-0.13.1.tar.xz>
  (SHA-256 `0e85c8702248b8dd64144f594147553650cdd0e4c951690345faace79c217c4d`)
- **License:** LGPL-2.0-or-later; text in [`bubblewrap-LICENSE`](bubblewrap-LICENSE)

```text
Copyright (C) 2016 Alexander Larsson
Copyright (C) 2017, 2018, 2019 Giuseppe Scrivano <giuseppe@scrivano.org>
Based on realpath.c Copyright (C) 1993 Rick Sladkey <jrs@world.std.com>
```

## passt

- **Bundled as:** `pasta` (`/usr/lib/kubetail-desktop/pasta`), in the `.deb` and the `.rpm`; and its
  AppArmor profile for `pasta`, adapted as `kubetail-desktop-pasta`, in the `.deb`
- **Version:** 2026_10_02.cba3570
- **Upstream:** <https://passt.top/>
- **Source:** `passt/` in the release's embedded-sources archive, unpacked from
  <https://passt.top/passt/snapshot/passt-2026_10_02.cba3570.tar.xz>
  (SHA-256 `aa75616cc43925f0bd6c05bcfbbaecd77397c03d7bd23591b34ef535be5dfcdc`)
- **License:** GPL-2.0-or-later, with parts under BSD-3-Clause; texts in
  [`passt-LICENSE-GPL-2.0-or-later`](passt-LICENSE-GPL-2.0-or-later) and
  [`passt-LICENSE-BSD-3-Clause`](passt-LICENSE-BSD-3-Clause)

```text
Copyright (c) 2020-2026 Red Hat GmbH
Authors: Stefano Brivio <sbrivio@redhat.com>, David Gibson <david@gibson.dropbear.id.au>,
Laurent Vivier <lvivier@redhat.com>
Copyright (C) 2010 Red Hat, Inc.

Copyright (c) 2014-2016, The Regents of the University of California.
Copyright (c) 2016-2017, Nefeli Networks, Inc.

Copyright IBM, Corp. 2007
Copyright (c) 2016 Red Hat, Inc.

Copyright (c) 2012-2021 Jean-Philippe Aumasson <jeanphilippe.aumasson@gmail.com>
Copyright (c) 2012-2014 Daniel J. Bernstein <djb@cr.yp.to>
Copyright (C) 2016 Jason A. Donenfeld <Jason@zx2c4.com>. All Rights Reserved.
```

## AppArmor

- **Bundled as:** its `bwrap-userns-restrict` profile, adapted as `kubetail-desktop-bwrap`, in the `.deb`
- **Version:** 4.1.8 (`profiles/apparmor/profiles/extras/bwrap-userns-restrict`)
- **Upstream:** <https://gitlab.com/apparmor/apparmor>
- **Source:** the profile is its own source; the `.deb` carries it at
  `/usr/lib/kubetail-desktop/apparmor/kubetail-desktop-bwrap`
- **License:** GPL-2.0; text in [`apparmor-LICENSE`](apparmor-LICENSE)

```text
Copyright AppArmor's authors.
```
