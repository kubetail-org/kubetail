# Copyright 2021-2025 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

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

QA_PREBUILT="usr/bin/kubetail"

src_compile() {
	chmod +x kubetail

	./kubetail completion bash > "kubetail.bash" || die
	./kubetail completion zsh > "kubetail.zsh" || die
	./kubetail completion fish > "kubetail.fish" || die
}

src_install() {
	dobin kubetail || die

	newbashcomp "kubetail.bash" kubetail
	newzshcomp "kubetail.zsh" "_kubetail"
	dofishcomp "kubetail.fish"
}
