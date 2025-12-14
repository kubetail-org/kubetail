// Copyright 2024-2025 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import { cssID } from './util';

describe('cssID', () => {
  it('handles simple alphanumeric inputs', () => {
    const result = cssID('default', 'mypod', 'mycontainer');
    expect(result).toBe('default__002fmypod__002fmycontainer');
  });

  it('encodes forward slashes as __002f', () => {
    const result = cssID('ns', 'pod', 'container');
    expect(result).toBe('ns__002fpod__002fcontainer');
  });

  it('handles namespace with hyphens', () => {
    const result = cssID('kube-system', 'pod-name', 'container-name');
    expect(result).toBe('kube__002dsystem__002fpod__002dname__002fcontainer__002dname');
  });

  it('converts uppercase letters to lowercase with underscore prefix', () => {
    const result = cssID('MyNamespace', 'MyPod', 'MyContainer');
    expect(result).toBe('_my_namespace__002f_my_pod__002f_my_container');
  });

  it('handles mixed case inputs', () => {
    const result = cssID('TestNS', 'TestPod', 'TestContainer');
    expect(result).toBe('_test_n_s__002f_test_pod__002f_test_container');
  });

  it('converts spaces to hyphens', () => {
    const result = cssID('my namespace', 'my pod', 'my container');
    expect(result).toBe('my-namespace__002fmy-pod__002fmy-container');
  });

  it('encodes special characters with hex codes', () => {
    const result = cssID('ns@test', 'pod#1', 'container$2');
    expect(result).toBe('ns__0040test__002fpod__00231__002fcontainer__00242');
  });

  it('handles dots in names', () => {
    const result = cssID('namespace.v1', 'pod.app', 'container.sidecar');
    expect(result).toBe('namespace__002ev1__002fpod__002eapp__002fcontainer__002esidecar');
  });

  it('handles underscores in names', () => {
    const result = cssID('my_namespace', 'my_pod', 'my_container');
    expect(result).toBe('my__005fnamespace__002fmy__005fpod__002fmy__005fcontainer');
  });

  it('handles colons in names', () => {
    const result = cssID('namespace:prod', 'pod:v2', 'container:main');
    expect(result).toBe('namespace__003aprod__002fpod__003av2__002fcontainer__003amain');
  });

  it('handles empty strings', () => {
    const result = cssID('', '', '');
    expect(result).toBe('__002f__002f');
  });

  it('handles complex real-world kubernetes names', () => {
    const result = cssID('production-v2', 'nginx-deployment-5d59d67564-abcde', 'nginx-proxy');
    expect(result).toBe(
      'production__002dv2__002fnginx__002ddeployment__002d5d59d67564__002dabcde__002fnginx__002dproxy',
    );
  });

  it('handles names with numbers', () => {
    const result = cssID('namespace1', 'pod123', 'container456');
    expect(result).toBe('namespace1__002fpod123__002fcontainer456');
  });

  it('handles unicode characters with hex encoding', () => {
    const result = cssID('ns', 'pod', 'container-\u00e9');
    expect(result).toBe('ns__002fpod__002fcontainer__002d__00e9');
  });

  it('handles parentheses and brackets', () => {
    const result = cssID('ns(test)', 'pod[1]', 'container{a}');
    expect(result).toBe('ns__0028test__0029__002fpod__005b1__005d__002fcontainer__007ba__007d');
  });
});
