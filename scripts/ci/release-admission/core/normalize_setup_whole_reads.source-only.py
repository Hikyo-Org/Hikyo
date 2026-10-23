"""Pure strict native setup-policy read normalization. No I/O or current clock."""
import hashlib
import json
import math
import re
from datetime import datetime, timezone
from urllib.parse import quote, unquote_to_bytes
import xml.etree.ElementTree as ET

ARN = "arn:aws:iam::099256073061:policy/hikyo-158-setup-policy"
POLICY_ID = "ANPAROHA7PNSTPQUQEUTR"
MAIN = "85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9"
VENDORS = "e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9"
NS = "{https://iam.amazonaws.com/doc/2010-05-08/}"
STAGES = {"beforeDelete": (["v1", "v2", "v3", "v4", "v5"], "v5"),
          "afterDelete": (["v2", "v3", "v4", "v5"], "v5"),
          "activated": (["v2", "v3", "v4", "v5", "v6"], "v6"),
          "restored": (["v2", "v3", "v4", "v5", "v6"], "v5")}
HASHES = {"v1": "75cad189d23a96fa441aca47de0834ec6b8c3dbeac2d19a5d929a485b732e4a8",
          "v2": "5d0cb52fdd94cb0bc68db4e2ffc5c5570a62c9a459da7c5399fda68e05e7afd7",
          "v3": "fc272c1a9676cdbf4f10e991f10750b4d00fa5ece69d81be0d4f88226dd02537",
          "v4": "c5301c647e710f04e8726713fa474f1b44e19ce1176b1b2e6560478c1db38579",
          "v5": "5d0cb52fdd94cb0bc68db4e2ffc5c5570a62c9a459da7c5399fda68e05e7afd7"}
MANIFEST_KEYS = {"schema", "stage", "account", "region", "policyARN", "policyId", "notBeforeMS", "expiresAtMS", "reviewedPreparationSHA256", "publicMainSHA256", "publicVendorsSHA256"}
ROW_KEYS = {"schema", "stage", "account", "region", "action", "versionId", "policyARN", "policyId", "slot", "requestBodyBytes", "requestBodySHA256", "status", "endpoint", "sentAtMS", "receivedAtMS", "rawResponseBytes", "rawResponseSHA256", "rawResponse", "manifestSHA256", "notBeforeMS", "expiresAtMS", "reviewedPreparationSHA256", "publicMainSHA256", "publicVendorsSHA256", "ownerIdentityVerifiedByObserver", "publicSourceBytesVerifiedByObserver", "providerAPIsCalledByObserver", "policyDocumentsDecodedByObserver"}


def require(condition, message):
    if not condition:
        raise ValueError("hikyo158-next-whole-read:" + message)


def keys(value, expected):
    require(type(value) is dict and set(value) == set(expected), "closed-fields")


def sha(value):
    return hashlib.sha256(value).hexdigest()


def sha_like(value):
    return type(value) is str and re.fullmatch(r"[a-f0-9]{64}", value) is not None


def canonical(value):
    def finite(item, depth=0):
        require(depth <= 30, "json-depth")
        if type(item) is float:
            require(math.isfinite(item), "nonfinite-json")
        elif type(item) is list:
            for child in item:
                finite(child, depth + 1)
        elif type(item) is dict:
            require(all(type(key) is str for key in item), "json-keys")
            for child in item.values():
                finite(child, depth + 1)
        else:
            require(item is None or type(item) in (str, int, bool), "json-values")
    finite(value)
    result = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False)
    require(len(result.encode("utf-8")) <= 262144, "json-size")
    return result


def strict_json(raw):
    require(type(raw) is str and len(raw.encode("utf-8")) <= 32768, "bounded-json")
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate-json-key")
            result[key] = value
        return result
    def constant(_):
        raise ValueError("hikyo158-next-whole-read:nonfinite-json")
    value = json.loads(raw, object_pairs_hook=unique, parse_constant=constant)
    canonical(value)
    return value


def policy(encoded):
    require(type(encoded) is str and 0 < len(encoded) <= 32768 and not re.search(r"%(?![0-9a-fA-F]{2})", encoded), "policy-uri")
    # Percent decoding happens exactly once; '+' is literal URI content.
    value = strict_json(unquote_to_bytes(encoded).decode("utf-8", errors="strict"))
    require(type(value) is dict and set(value) == {"Version", "Statement"} and value["Version"] == "2012-10-17" and type(value["Statement"]) is list, "policy-document")
    return value


def instant(raw):
    require(type(raw) is str and re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?Z", raw), "utc-instant")
    return datetime.fromisoformat(raw.replace("Z", "+00:00"))


def xml(raw, action):
    require(not re.search(r"<!\s*(?:DOCTYPE|ENTITY)", raw, re.I), "xml-dtd-entity")
    parser = ET.XMLParser(target=ET.TreeBuilder(insert_comments=True, insert_pis=True))
    root = ET.fromstring(raw, parser=parser)
    def tree(node, depth=0):
        require(depth <= 24 and type(node.tag) is str and node.tag.startswith(NS) and node.tag.count("}") == 1 and not node.attrib, "xml-namespace-attributes-depth")
        children = list(node)
        require(not (node.tail or "").strip(), "xml-tail")
        if not children:
            return node.text or ""
        require(not (node.text or "").strip(), "xml-mixed-content")
        names = [child.tag[len(NS):] if type(child.tag) is str and child.tag.startswith(NS) else None for child in children]
        if names == ["member"] * len(children):
            return [tree(child, depth + 1) for child in children]
        require(None not in names and "member" not in names and len(names) == len(set(names)), "xml-duplicate-foreign")
        return {name: tree(child, depth + 1) for name, child in zip(names, children)}
    require(root.tag == NS + action + "Response", "xml-operation")
    result = tree(root)
    keys(result, {action + "Result", "ResponseMetadata"})
    keys(result["ResponseMetadata"], {"RequestId"})
    request_id = result["ResponseMetadata"]["RequestId"]
    require(type(request_id) is str and re.fullmatch(r"[A-Za-z0-9-]{1,128}", request_id), "xml-request-id")
    require(type(result[action + "Result"]) is dict, "xml-result")
    return result[action + "Result"]


def decimal_integer(raw):
    require(type(raw) is str and re.fullmatch(r"0|[1-9][0-9]{0,8}", raw), "integer-metadata")
    return int(raw)


def normalize(manifest, rows, owner_context_sha256, reviewed_v6_sha256, explicit_now_ms):
    keys(manifest, MANIFEST_KEYS)
    require(manifest["schema"] == "hikyo-158/next-setup-whole-read-manifest/v1" and manifest["stage"] in STAGES and manifest["account"] == "099256073061" and manifest["region"] == "eu-central-1" and manifest["policyARN"] == ARN and manifest["policyId"] == POLICY_ID and manifest["publicMainSHA256"] == MAIN and manifest["publicVendorsSHA256"] == VENDORS and sha_like(manifest["reviewedPreparationSHA256"]), "manifest-binding")
    require(type(explicit_now_ms) is int and type(manifest["notBeforeMS"]) is int and type(manifest["expiresAtMS"]) is int and 0 <= explicit_now_ms - manifest["notBeforeMS"] <= 300000 and explicit_now_ms < manifest["expiresAtMS"] <= manifest["notBeforeMS"] + 300000 and sha_like(owner_context_sha256), "fresh-snapshot-window")
    ids, default = STAGES[manifest["stage"]]
    require(sha_like(reviewed_v6_sha256) if "v6" in ids else reviewed_v6_sha256 is None, "late-bound-v6-review")
    expected = {"GetPolicy", "ListPolicyVersions", *["GetPolicyVersion." + version for version in ids]}
    require(type(rows) is list and len(rows) == len(expected), "complete-exact-read-count")
    manifest_sha = sha(canonical(manifest).encode())
    results, times, raw_custody = {}, [], {}
    for row in rows:
        keys(row, ROW_KEYS)
        slot = row["slot"]
        require(slot in expected and slot not in results and row["action"] in ("GetPolicy", "ListPolicyVersions", "GetPolicyVersion") and row["versionId"] == (slot.split(".")[1] if "." in slot else None) and slot == row["action"] + ("." + row["versionId"] if row["versionId"] else ""), "unique-exact-operation")
        require(row["schema"] == "hikyo-158/next-setup-native-read-response/v1" and row["stage"] == manifest["stage"] and row["account"] == "099256073061" and row["region"] == "eu-central-1" and row["policyARN"] == ARN and row["policyId"] == POLICY_ID and type(row["status"]) is int and row["status"] == 200 and row["endpoint"] == "https://iam.amazonaws.com/" and row["manifestSHA256"] == manifest_sha and all(row[key] == manifest[key] for key in ("notBeforeMS", "expiresAtMS", "reviewedPreparationSHA256", "publicMainSHA256", "publicVendorsSHA256")) and all(row[key] is False for key in ("ownerIdentityVerifiedByObserver", "publicSourceBytesVerifiedByObserver", "providerAPIsCalledByObserver", "policyDocumentsDecodedByObserver")), "receipt-provenance")
        sent, received = row["sentAtMS"], row["receivedAtMS"]
        require(type(sent) is int and type(received) is int and manifest["notBeforeMS"] <= sent <= received <= explicit_now_ms and explicit_now_ms - sent <= 300000 and received - sent <= 20000, "receipt-clock")
        body = "Action=" + row["action"] + "&Version=2010-05-08&PolicyArn=" + quote(ARN, safe="-._~") + ("&VersionId=" + row["versionId"] if row["versionId"] else "")
        bodies = [body, body + "&MaxItems=1000"] if row["action"] == "ListPolicyVersions" else [body]
        require(type(row["requestBodyBytes"]) is int and any(row["requestBodyBytes"] == len(candidate.encode()) and row["requestBodySHA256"] == sha(candidate.encode()) for candidate in bodies), "whole-request-wire")
        raw = row["rawResponse"]
        require(type(raw) is str and 2 <= len(raw.encode()) <= 32768 and type(row["rawResponseBytes"]) is int and row["rawResponseBytes"] == len(raw.encode()) and row["rawResponseSHA256"] == sha(raw.encode()), "raw-response-hash")
        results[slot] = xml(raw, row["action"])
        times.append(sent)
        raw_custody[slot] = sha(canonical(row).encode())
    require(set(results) == expected, "all-stage-reads-required")
    keys(results["GetPolicy"], {"Policy"})
    meta = results["GetPolicy"]["Policy"]
    required = {"PolicyName", "PolicyId", "Arn", "Path", "DefaultVersionId", "AttachmentCount", "PermissionsBoundaryUsageCount", "IsAttachable", "CreateDate", "UpdateDate"}
    require(type(meta) is dict and required <= set(meta) <= required | {"Description", "Tags"}, "full-policy-metadata")
    require(meta["PolicyName"] == "hikyo-158-setup-policy" and meta["PolicyId"] == POLICY_ID and meta["Arn"] == ARN and meta["Path"] == "/" and meta["DefaultVersionId"] == default and meta["IsAttachable"] == "true" and decimal_integer(meta["AttachmentCount"]) == 1 and decimal_integer(meta["PermissionsBoundaryUsageCount"]) == 0, "immutable-policy-identity-usage-default")
    if "Description" in meta:
        require(type(meta["Description"]) is str, "policy-description")
    if "Tags" in meta:
        require(meta["Tags"] == "" or type(meta["Tags"]) is list, "policy-tags")
        for tag in ([] if meta["Tags"] == "" else meta["Tags"]):
            keys(tag, {"Key", "Value"})
            require(type(tag["Key"]) is str and type(tag["Value"]) is str, "policy-tag-fields")
    now = datetime.fromtimestamp(explicit_now_ms / 1000, tz=timezone.utc)
    require(instant(meta["CreateDate"]) <= instant(meta["UpdateDate"]) <= now, "policy-metadata-dates")
    listing = results["ListPolicyVersions"]
    keys(listing, {"Versions", "IsTruncated"})
    require(listing["IsTruncated"] == "false" and type(listing["Versions"]) is list and len(listing["Versions"]) == len(ids), "complete-untruncated-version-inventory")
    inventory = {}
    for version in listing["Versions"]:
        keys(version, {"VersionId", "IsDefaultVersion", "CreateDate"})
        require(version["VersionId"] in ids and version["VersionId"] not in inventory and version["IsDefaultVersion"] == ("true" if version["VersionId"] == default else "false") and instant(version["CreateDate"]) <= now, "exact-version-metadata")
        inventory[version["VersionId"]] = version
    versions, documents_sha, version_metadata = {}, {}, {}
    for version_id in ids:
        result = results["GetPolicyVersion." + version_id]
        keys(result, {"PolicyVersion"})
        version = result["PolicyVersion"]
        keys(version, {"Document", "VersionId", "IsDefaultVersion", "CreateDate"})
        require(version["VersionId"] == version_id and version["IsDefaultVersion"] == inventory[version_id]["IsDefaultVersion"] and instant(version["CreateDate"]) == instant(inventory[version_id]["CreateDate"]), "whole-version-metadata-agrees")
        document = policy(version["Document"])
        document_sha = sha(canonical(document).encode())
        require(document_sha == (reviewed_v6_sha256 if version_id == "v6" else HASHES[version_id]), "whole-reviewed-policy-document")
        versions[version_id] = {"document": document, "isDefault": version_id == default}
        documents_sha[version_id] = document_sha
        version_metadata[version_id] = {"CreateDate": version["CreateDate"], "inventoryCreateDate": inventory[version_id]["CreateDate"], "encodedDocumentSHA256": sha(version["Document"].encode())}
    proof = {"schema": "hikyo-158/next-setup-whole-policy-proof/v1", "account": "099256073061", "observedAtMS": min(times), "ownerContextSHA256": owner_context_sha256,
             "policy": {"arn": ARN, "policyId": POLICY_ID, "defaultVersionId": default, "versions": versions}}
    return {"schema": "hikyo-158/next-setup-whole-read-normalized/v1", "stage": manifest["stage"], "proof": proof,
            "provenance": {"manifestSHA256": manifest_sha, "rawReceiptCanonicalSHA256s": raw_custody, "policyMetadata": meta, "versionMetadata": version_metadata,
                           "documentCanonicalSHA256s": documents_sha, "earliestActualSentAtMS": min(times)},
            "ownerIdentityAuthenticatedByNormalizer": False, "v6FullSemanticsValidatedByNormalizer": False, "providerAPIsCalledByNormalizer": False}
