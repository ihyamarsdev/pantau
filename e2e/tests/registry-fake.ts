import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

// A tiny stand-in for registry.terraform.io, shaped like the real responses, so tests never touch the network.
const MODULE_VERSIONS = ['1.0.0', '1.1.0', '1.2.0'];
const PROVIDER_VERSIONS = ['4.0.0', '5.0.0', '5.1.0'];

const moduleMeta = {
  id: 'acme/vpc/aws/1.2.0', owner: '', namespace: 'acme', name: 'vpc', version: '1.2.0', provider: 'aws',
  description: 'Acme VPC module', source: 'https://example.invalid/acme/vpc', tag: 'v1.2.0',
  published_at: '2026-01-01T00:00:00Z', downloads: 4242, verified: true,
};

const moduleDetail = {
  ...moduleMeta,
  root: {
    path: '', name: 'vpc', readme: '# Acme VPC\n<script>window.pwned = true</script>\nCreates a **VPC**.\n\n[bad](javascript:alert(1)) [ok](https://example.com/x)\n\n```hcl\nmodule "vpc" {}\n```\n', empty: false,
    inputs: [
      { name: 'cidr', type: 'string', description: 'CIDR block of the VPC', default: '', required: true },
      { name: 'enable_dns', type: 'bool', description: 'Enable DNS support', default: 'true', required: false },
    ],
    outputs: [{ name: 'vpc_id', description: 'ID of the VPC' }],
    dependencies: [], provider_dependencies: [], resources: [],
  },
  submodules: [], examples: [], providers: ['aws'], versions: MODULE_VERSIONS, deprecation: null,
};

const providerDetail = {
  id: 'hashicorp/aws/5.1.0', owner: 'hashicorp', namespace: 'hashicorp', name: 'aws', alias: 'aws', version: '5.1.0',
  tag: 'v5.1.0', description: 'The AWS provider', source: 'https://example.invalid/aws', published_at: '2026-01-01T00:00:00Z',
  downloads: 99, tier: 'official', logo_url: '', versions: PROVIDER_VERSIONS, docs: [],
};

function route(url: URL): { status: number; body: unknown } {
  const path = url.pathname;
  if (path === '/v1/modules/search') {
    return { status: 200, body: { meta: { limit: 20, current_offset: 0, next_offset: null }, modules: [moduleMeta] } };
  }
  if (path === '/v1/modules/acme/vpc/aws' || path === '/v1/modules/acme/vpc/aws/1.2.0') return { status: 200, body: moduleDetail };
  if (path === '/v1/modules/acme/vpc/aws/versions') {
    return { status: 200, body: { modules: [{ source: 'acme/vpc/aws', versions: MODULE_VERSIONS.map((version) => ({ version })) }] } };
  }
  if (path === '/v2/providers') {
    return {
      status: 200,
      body: { data: [{ type: 'providers', id: '1', attributes: { name: 'aws', namespace: 'hashicorp', 'full-name': 'hashicorp/aws', description: 'The AWS provider', downloads: 99, tier: 'official', source: 'https://example.invalid/aws' } }], meta: { pagination: { 'current-page': 1, 'next-page': null } } },
    };
  }
  if (path === '/v1/providers/hashicorp/aws') return { status: 200, body: providerDetail };
  if (path === '/v1/providers/hashicorp/aws/versions') {
    return { status: 200, body: { id: 'hashicorp/aws', versions: PROVIDER_VERSIONS.map((version) => ({ version, protocols: ['5.0'] })) } };
  }
  return { status: 404, body: { errors: ['not found'] } };
}

export async function startFakeRegistry(): Promise<{ url: string; close(): Promise<void> }> {
  const server: Server = createServer((req, res) => {
    const { status, body } = route(new URL(req.url ?? '/', 'http://fake'));
    res.writeHead(status, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(body));
  });
  const listening = Promise.withResolvers<void>();
  server.listen(0, '127.0.0.1', () => listening.resolve());
  await listening.promise;
  // Listening on a TCP port, so address() is an AddressInfo rather than a pipe name.
  const address = server.address() as AddressInfo;
  const close = async () => {
    const closed = Promise.withResolvers<void>();
    server.close(() => closed.resolve());
    await closed.promise;
  };
  return { url: `http://127.0.0.1:${address.port}`, close };
}
