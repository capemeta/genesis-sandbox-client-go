"""从当前服务协议结构化生成 SDK 单文件快照。"""
import argparse
from pathlib import Path

import yaml


def bundle(source: Path) -> dict:
    document = yaml.safe_load(source.read_text(encoding="utf-8"))
    definitions = document["components"]["schemas"]
    external_schemas = {
        filename: yaml.safe_load((source.parent / filename).read_text(encoding="utf-8"))
        for filename in ("shared-storage.openapi.yaml", "execution-governance.openapi.yaml")
    }

    def visit(value, origin=None):
        if isinstance(value, dict):
            reference = value.get("$ref", "")
            if origin and reference.startswith("#/") and not reference.startswith("#/components/"):
                reference = f"./{origin}{reference}"
            if reference.startswith("./"):
                filename, name = reference[2:].split("#/", 1)
                schema = external_schemas[filename][name]
                if name in definitions and definitions[name] != schema:
                    raise ValueError(f"协议 Schema 名称冲突: {name}")
                definitions[name] = schema
                value["$ref"] = f"#/components/schemas/{name}"
                visit(schema, filename)
            for nested in list(value.values()):
                visit(nested, origin)
        elif isinstance(value, list):
            for nested in value:
                visit(nested, origin)

    visit(document)
    return document


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    target = Path(__file__).resolve().parents[1] / "api" / "openapi.yaml"
    target.write_text(yaml.safe_dump(bundle(args.source), allow_unicode=True, sort_keys=False), encoding="utf-8", newline="\n")
