Name:           kubetail
Version:        %{version} 
Release:        %{release}%{?dist}
Summary:        Real-time logging dashboard for Kubernetes 

License:        Apache-2.0
URL:            https://github.com/kubetail-org/kubetail
Source0:        %{name}-%{version}.tar.gz


%description
Kubetail is a general-purpose logging dashboard for Kubernetes,
optimized for tailing logs across across multi-container workloads
in real-time. With Kubetail, you can view logs from all the containers
in a workload (e.g. Deployment or DaemonSet) merged into a single,
chronological timeline, delivered to your browser or terminal.


%prep
%setup -q

%install
mkdir -p %{buildroot}%{_bindir}
install -m 755 %{_sourcedir}/%{name}-%{version}/kubetail %{buildroot}%{_bindir}/%{name}

%files
%{_bindir}/%{name}
