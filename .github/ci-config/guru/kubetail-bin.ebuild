# Copyright 2021-2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

MY_PN="${PN%-bin}"

inherit shell-completion

DESCRIPTION="Real-time logging dashboard for Kubernetes"

HOMEPAGE="https://github.com/kubetail-org/kubetail"

SRC_URI="
amd64? ( https://github.com/kubetail-org/kubetail/releases/download/cli%2Fv${PV}/kubetail-linux-amd64.tar.gz
-> ${P}-linux-amd64.tar.gz )
arm64? ( https://github.com/kubetail-org/kubetail/releases/download/cli%2Fv${PV}/kubetail-linux-arm64.tar.gz
-> ${P}-linux-arm64.tar.gz )
"

S="${WORKDIR}"

LICENSE="Apache-2.0"

SLOT="0"

KEYWORDS="~amd64 ~arm64"

QA_PREBUILT="usr/bin/${MY_PN}"

src_compile() {
	chmod +x "${MY_PN}"

	"./${MY_PN}" completion bash > "${MY_PN}.bash" || die
	"./${MY_PN}" completion zsh > "${MY_PN}.zsh" || die
	"./${MY_PN}" completion fish > "${MY_PN}.fish" || die
}

src_install() {
	dobin "${MY_PN}"

	newbashcomp "${MY_PN}.bash" "${MY_PN}"
	newzshcomp "${MY_PN}.zsh" "_${MY_PN}"
	dofishcomp "${MY_PN}.fish"
}
