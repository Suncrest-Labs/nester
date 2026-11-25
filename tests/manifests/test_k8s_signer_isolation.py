"""Structural checks for the signer's Kubernetes manifests (nester#1530).

The signer holds the Stellar operator key, so its pod security and its network
boundaries are the controls worth asserting. There is no cluster harness in this
repository, so these tests parse the committed manifests and check the fields a
live `kubectl apply` would act on. They do not replace a cluster test: a CNI
that ignores NetworkPolicy, or a kubelet that cannot reach a probe through the
policy, will only show up against a real cluster.

Run from the repository root:

    python -m pytest tests/manifests -q

Requires PyYAML (`import yaml`); no other test dependency is added.
"""

from __future__ import annotations

import ipaddress
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
PRODUCTION_MANIFEST = str(REPO_ROOT / "deploy/production/k8s-production.yaml")
STAGING_MANIFEST = str(REPO_ROOT / "deploy/staging/k8s-staging.yaml")

SIGNER_PORT = 8443

ENVIRONMENTS = [
    pytest.param(
        PRODUCTION_MANIFEST,
        "nester-production",
        "production",
        "nester-signer-production",
        "nester-signer-production",
        id="production",
    ),
    pytest.param(
        STAGING_MANIFEST,
        "nester-staging",
        "staging",
        "nester-signer-staging",
        "nester-signer-staging",
        id="staging",
    ),
]


def load(path: str) -> list[dict]:
    with open(path, encoding="utf-8") as fh:
        return [doc for doc in yaml.safe_load_all(fh) if doc]


def find(docs: list[dict], kind: str, name: str) -> dict:
    matches = [d for d in docs if d.get("kind") == kind and d["metadata"]["name"] == name]
    assert len(matches) == 1, f"expected exactly one {kind}/{name}, found {len(matches)}"
    return matches[0]


def signer_labels(env: str) -> dict[str, str]:
    return {"app": "nester-signer", "env": env}


def api_labels(env: str) -> dict[str, str]:
    return {"app": "nester-api", "env": env}


def container(deployment: dict, name: str) -> dict:
    containers = deployment["spec"]["template"]["spec"]["containers"]
    matches = [c for c in containers if c["name"] == name]
    assert matches, f"container {name!r} not found in {deployment['metadata']['name']}"
    return matches[0]


def env_value(container_spec: dict, name: str) -> dict | None:
    for entry in container_spec.get("env", []):
        if entry["name"] == name:
            return entry
    return None


def selects(pod_selector: dict | None, labels: dict[str, str]) -> bool:
    """True when a NetworkPolicy podSelector can match the given pod labels.

    An empty selector selects every pod, and matchExpressions are treated
    conservatively as selecting too: a policy we cannot prove excludes the
    signer must be inspected, not ignored.
    """
    if pod_selector is None or not pod_selector:
        return True
    if pod_selector.get("matchExpressions"):
        return True
    return all(labels.get(k) == v for k, v in pod_selector.get("matchLabels", {}).items())


def policies_for_signer(docs: list[dict], namespace: str, labels: dict[str, str]) -> list[dict]:
    policies = [d for d in docs if d.get("kind") == "NetworkPolicy"]
    return [
        p
        for p in policies
        if p["metadata"].get("namespace") == namespace and selects(p["spec"].get("podSelector"), labels)
    ]


@pytest.mark.parametrize("path,namespace,env,deployment_name,service_name", ENVIRONMENTS)
def test_signer_pod_security(path, namespace, env, deployment_name, service_name):
    docs = load(path)
    deployment = find(docs, "Deployment", deployment_name)
    assert deployment["metadata"]["namespace"] == namespace

    pod_spec = deployment["spec"]["template"]["spec"]
    pod_sc = pod_spec["securityContext"]
    assert pod_sc["runAsNonRoot"] is True
    # distroless/static:nonroot; the ids must match the image's own user.
    assert pod_sc["runAsUser"] == 65532
    assert pod_sc["runAsGroup"] == 65532
    assert pod_sc["fsGroup"] == 65532
    assert pod_sc["seccompProfile"]["type"] == "RuntimeDefault"
    assert pod_spec["automountServiceAccountToken"] is False

    signer = container(deployment, "signer")
    sc = signer["securityContext"]
    assert sc["runAsNonRoot"] is True
    assert sc["readOnlyRootFilesystem"] is True
    assert sc["allowPrivilegeEscalation"] is False
    assert "ALL" in sc["capabilities"]["drop"]

    resources = signer["resources"]
    for section in ("requests", "limits"):
        assert resources[section]["cpu"], f"missing cpu {section}"
        assert resources[section]["memory"], f"missing memory {section}"

    port = next(p for p in signer["ports"] if p["containerPort"] == SIGNER_PORT)
    assert port["name"] == "signer-mtls"


@pytest.mark.parametrize("path,namespace,env,deployment_name,service_name", ENVIRONMENTS)
def test_signer_secrets_are_not_inlined(path, namespace, env, deployment_name, service_name):
    docs = load(path)
    signer = container(find(docs, "Deployment", deployment_name), "signer")

    # The key is the one credential this container exists to hold. It must come
    # from a Secret reference, never a literal committed to git.
    operator_secret = env_value(signer, "STELLAR_OPERATOR_SECRET")
    assert operator_secret is not None, "signer has no STELLAR_OPERATOR_SECRET"
    assert "value" not in operator_secret
    assert operator_secret["valueFrom"]["secretKeyRef"]["name"]

    # The API must not hold the signing key.
    api = container(find(docs, "Deployment", "nester-api-" + env), "api")
    assert env_value(api, "STELLAR_OPERATOR_SECRET") is None


@pytest.mark.parametrize("path,namespace,env,deployment_name,service_name", ENVIRONMENTS)
def test_signer_service_targets_the_mtls_port(path, namespace, env, deployment_name, service_name):
    docs = load(path)
    service = find(docs, "Service", service_name)
    assert service["metadata"]["namespace"] == namespace
    assert service["spec"]["selector"] == signer_labels(env)
    assert service["spec"]["type"] == "ClusterIP"

    ports = service["spec"]["ports"]
    assert len(ports) == 1
    assert ports[0]["port"] == SIGNER_PORT
    assert ports[0]["targetPort"] == "signer-mtls"
    assert ports[0]["protocol"] == "TCP"


@pytest.mark.parametrize("path,namespace,env,deployment_name,service_name", ENVIRONMENTS)
def test_default_deny_covers_both_directions(path, namespace, env, deployment_name, service_name):
    policies = policies_for_signer(load(path), namespace, signer_labels(env))
    deny = [p for p in policies if p["metadata"]["name"] == "nester-signer-default-deny"]
    assert len(deny) == 1
    deny = deny[0]
    assert set(deny["spec"]["policyTypes"]) == {"Ingress", "Egress"}
    # No rules at all: the other policies are the only ways back in or out.
    assert not deny["spec"].get("ingress")
    assert not deny["spec"].get("egress")


@pytest.mark.parametrize("path,namespace,env,deployment_name,service_name", ENVIRONMENTS)
def test_only_api_pods_can_reach_the_signer(path, namespace, env, deployment_name, service_name):
    docs = load(path)
    labels = signer_labels(env)
    policies = policies_for_signer(docs, namespace, labels)
    allowed_names = {
        "nester-signer-default-deny",
        "nester-signer-allow-api",
        "nester-signer-egress",
    }
    unexpected = [p["metadata"]["name"] for p in policies if p["metadata"]["name"] not in allowed_names]
    assert not unexpected, f"unreviewed policy also selects the signer pods: {unexpected}"

    allow = find(docs, "NetworkPolicy", "nester-signer-allow-api")
    assert allow["metadata"]["namespace"] == namespace
    assert allow["spec"]["podSelector"] == {"matchLabels": labels}
    assert allow["spec"]["policyTypes"] == ["Ingress"]
    assert len(allow["spec"]["ingress"]) == 1

    rule = allow["spec"]["ingress"][0]
    assert rule["ports"] == [{"protocol": "TCP", "port": SIGNER_PORT}]
    assert len(rule["from"]) == 1
    source = rule["from"][0]
    assert source["namespaceSelector"] == {
        "matchLabels": {"kubernetes.io/metadata.name": namespace}
    }
    assert source["podSelector"] == {"matchLabels": api_labels(env)}

    # Every ingress source in every signer-selecting policy must be that same
    # API selector, and every ingress port must be the signer port. An empty
    # `from` would mean "all pods" and is rejected by the comparison.
    for policy in policies:
        for ingress in policy["spec"].get("ingress", []) or []:
            ports = ingress.get("ports")
            assert ports, f"{policy['metadata']['name']} allows ingress on all ports"
            for p in ports:
                assert p["port"] == SIGNER_PORT, f"{policy['metadata']['name']} opens {p['port']}"
            sources = ingress.get("from")
            assert sources, f"{policy['metadata']['name']} allows ingress from all pods"
            assert sources == [source], f"{policy['metadata']['name']} allows another source"


@pytest.mark.parametrize("path,namespace,env,deployment_name,service_name", ENVIRONMENTS)
def test_signer_egress_is_dns_and_stellar_https_only(path, namespace, env, deployment_name, service_name):
    docs = load(path)
    egress = find(docs, "NetworkPolicy", "nester-signer-egress")
    assert egress["metadata"]["namespace"] == namespace
    assert egress["spec"]["podSelector"] == {"matchLabels": signer_labels(env)}
    assert egress["spec"]["policyTypes"] == ["Egress"]

    rules = egress["spec"]["egress"]
    assert len(rules) == 2, "an added egress rule widens the signer's reachable network"

    dns, https = rules
    assert dns["to"] == [
        {"namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "kube-system"}}}
    ]
    assert {(p["protocol"], p["port"]) for p in dns["ports"]} == {("UDP", 53), ("TCP", 53)}

    # Default-deny means these are the only reachable destinations. Vanilla
    # NetworkPolicy cannot name a host, so the check is: HTTPS only, and no
    # cluster-private address can match the block.
    assert [p["port"] for p in https["ports"]] == [443]
    assert all(p["protocol"] == "TCP" for p in https["ports"])
    assert len(https["to"]) == 1
    block = https["to"][0]["ipBlock"]
    assert block["cidr"] == "0.0.0.0/0"
    cluster_ranges = [
        ipaddress.ip_network("10.0.0.0/8"),
        ipaddress.ip_network("172.16.0.0/12"),
        ipaddress.ip_network("192.168.0.0/16"),
        ipaddress.ip_network("169.254.0.0/16"),
    ]
    excluded = [ipaddress.ip_network(cidr) for cidr in block["except"]]
    # The egress rule must carve the cluster's private ranges out of the public
    # block; otherwise the signer could reach the database or the API.
    for private in cluster_ranges:
        assert any(private.subnet_of(net) for net in excluded), (
            f"{private} is not excluded from the signer's egress allowlist"
        )


def test_staging_api_workload_is_untouched():
    """Adding the signer must not remove or rename what staging already runs."""
    docs = load(STAGING_MANIFEST)
    api = find(docs, "Deployment", "nester-api-staging")
    assert container(api, "api")["ports"][0]["containerPort"] == 8080
    service = find(docs, "Service", "nester-api-staging")
    assert service["spec"]["ports"][0]["targetPort"] == 8080
