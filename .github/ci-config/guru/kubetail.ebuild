# Copyright 2021-2025 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

inherit shell-completion

DESCRIPTION="Real-time logging dashboard for Kubernetes"

HOMEPAGE="https://github.com/kubetail-org/kubetail"

SRC_URI="https://github.com/kubetail-org/kubetail/releases/download/cli%2Fv${PV}/kubetail-${PV}-vendored.tar.gz"

S="${WORKDIR}/kubetail-${PV}"

LICENSE="Apache-2.0"

SLOT="0"

KEYWORDS="~amd64 ~arm64"

BDEPEND="
	>=dev-lang/go-1.24.7
"

QA_PREBUILT="usr/bin/kubetail"

src_compile() {
	(
		cd modules/cli || die
		GOWORK=off CGO_ENABLED=0 go build \
			-mod=vendor \
			-ldflags "-s -w -X github.com/kubetail-org/kubetail/modules/cli/cmd.version=${PV}" \
			-o ../../bin/kubetail \
			. || die
	)

	./bin/kubetail completion bash > "kubetail.bash" || die
	./bin/kubetail completion zsh > "kubetail.zsh" || die
	./bin/kubetail completion fish > "kubetail.fish" || die
}

src_install() {
	dobin bin/kubetail || die

	newbashcomp "kubetail.bash" kubetail
	newzshcomp "kubetail.zsh" "_kubetail"
	dofishcomp "kubetail.fish"
}
