#!/usr/bin/env python3
"""Validate design tokens and generate deterministic Compose, CSS and Markdown files."""

import argparse
import hashlib
import json
import math
import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_SOURCE = ROOT / "spec" / "tokens"
STYLES = {"glass", "claude", "clay", "island", "lark", "native"}
LEVELS = {"error", "warn"}
KNOWN_FAILURES = "known-failures.json"
MODES = {"light", "dark"}
TYPES = {"color", "dimension", "duration", "fontWeight", "number", "string"}
NAME = re.compile(r"[a-z][a-zA-Z0-9]*\Z")
REFERENCE = re.compile(r"\{([a-zA-Z0-9.]+)\}\Z")
KEYWORDS = set("as break class continue do else false for fun if in interface is null object package return super this throw true try typealias typeof val var when while".split())


class TokenError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise TokenError(message)


def number(value):
    try:
        return type(value) in (int, float) and math.isfinite(value)
    except OverflowError:
        return False


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"JSON 键重复：{key}")
        result[key] = value
    return result


def validate_value(kind, value, path):
    if kind == "color":
        require(isinstance(value, dict) and set(value) == {"colorSpace", "components", "alpha"}, f"{path}：颜色须包含 colorSpace、components、alpha")
        require(value["colorSpace"] == "srgb", f"{path}：仅支持 sRGB")
        components = value["components"]
        require(isinstance(components, list) and len(components) == 3, f"{path}：需要三个 RGB 分量")
        require(all(number(v) and 0 <= v <= 1 for v in components + [value["alpha"]]), f"{path}：颜色分量须在 0…1 内")
    elif kind in ("dimension", "duration"):
        require(isinstance(value, dict) and set(value) == {"value", "unit"}, f"{path}：需要 value 和 unit")
        require(number(value["value"]), f"{path}：value 必须是有限数值")
        require(value["unit"] in ({"px"} if kind == "dimension" else {"ms", "s"}), f"{path}：不支持的单位")
        if kind == "duration" or not path.endswith((".offsetX", ".offsetY")):
            require(value["value"] >= 0, f"{path}：不能为负数")
        if kind == "duration":
            ms = value["value"] * (1000 if value["unit"] == "s" else 1)
            require(ms.is_integer() if isinstance(ms, float) else True, f"{path}：动效时长须为整毫秒")
            require(ms <= 2147483647, f"{path}：动效时长超出 Kotlin Int 范围")
    elif kind == "fontWeight":
        require(type(value) is int and 1 <= value <= 1000, f"{path}：字重须为 1…1000 的整数")
    elif kind == "number":
        require(number(value), f"{path}：需要有限数值")
    else:
        require(isinstance(value, str) and not any(c in value for c in "{}"), f"{path}：需要普通字符串，引用须指向整个 token")


class Catalog:
    def __init__(self, source=DEFAULT_SOURCE):
        self.source = Path(source)
        self.tokens = {}
        self.values = {}
        self.documents = {}
        self.rules = None
        self.known_failures = set()
        files = sorted(p for p in self.source.glob("*.json") if p.name != KNOWN_FAILURES)
        require(files, "token 目录中没有 JSON 文件")
        known = self.source / KNOWN_FAILURES
        if known.is_file():
            try:
                doc = json.loads(known.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
            except (ValueError, OSError) as error:
                raise TokenError(f"{KNOWN_FAILURES}：{error}") from error
            require(isinstance(doc, dict) and isinstance(doc.get("failures"), list), f"{KNOWN_FAILURES}：需要 failures 列表")
            for item in doc["failures"]:
                require(isinstance(item, dict) and all(isinstance(item.get(k), str) and item[k] for k in ("style", "mode", "rule", "reason")),
                        f"{KNOWN_FAILURES}：每项要有 style、mode、rule、reason")
                self.known_failures.add((item["style"], item["mode"], item["rule"]))
        for path in files:
            try:
                doc = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
            except (ValueError, OSError) as error:
                raise TokenError(f"{path.name}：{error}") from error
            self.documents[path.name] = doc
            require(isinstance(doc, dict), f"{path.name}：顶层必须是对象")
            if path.name == "rules.json":
                require(set(doc) == {"$description", "$extensions"}, "rules.json：需要 $description 和 $extensions")
                require(isinstance(doc["$description"], str), "rules.json：$description 必须是字符串")
                ext = doc["$extensions"]
                require(isinstance(ext, dict) and set(ext) == {"persona"}, "rules.json：需要 persona 扩展")
                self.rules = ext["persona"]
            else:
                self._walk(doc, "")
        require(self.tokens, "没有 token")
        for path in sorted(self.tokens):
            self.resolve(path)
        self._validate_rules()
        self._validate_matrix()
        canonical = json.dumps(self.documents, sort_keys=True, ensure_ascii=False, separators=(",", ":"))
        self.digest = hashlib.sha256(canonical.encode()).hexdigest()

    def _walk(self, node, path):
        require(isinstance(node, dict) and node, f"{path or '根节点'}：需要非空对象")
        if "$value" in node:
            require(set(node) == {"$type", "$value", "$description"}, f"{path}：token 只接受 $type、$value、$description")
            require(isinstance(node["$type"], str) and node["$type"] in TYPES, f"{path}：不支持的 $type")
            require(isinstance(node["$description"], str) and node["$description"].strip(), f"{path}：缺少来源说明")
            require(path and path not in self.tokens, f"token 重复：{path}")
            self.tokens[path] = node
            return
        for key, value in node.items():
            if key == "$description":
                require(isinstance(value, str), f"{path}：$description 必须是字符串")
                continue
            require(NAME.fullmatch(key), f"{path}：非法名称 {key}")
            self._walk(value, f"{path}.{key}" if path else key)

    def resolve(self, path, stack=()):
        require(path in self.tokens, f"引用不存在：{path}")
        require(path not in stack, "循环引用：" + " → ".join((*stack, path)))
        if path in self.values:
            return self.values[path]
        node = self.tokens[path]
        value = node["$value"]
        match = REFERENCE.fullmatch(value) if isinstance(value, str) else None
        if match:
            target = match[1]
            value = self.resolve(target, (*stack, path))
            require(node["$type"] == self.tokens[target]["$type"], f"{path}：引用类型不一致：{target}")
        validate_value(node["$type"], value, path)
        self.values[path] = value
        return value

    def _validate_rules(self):
        config = self.rules
        require(isinstance(config, dict) and set(config) == {"styles", "modes", "contrast"}, "rules.json：需要 styles、modes、contrast")
        for key, expected in (("styles", STYLES), ("modes", MODES)):
            values = config[key]
            require(isinstance(values, list) and all(isinstance(v, str) for v in values) and len(values) == len(expected) and set(values) == expected, f"rules.json：{key} 未覆盖完整矩阵")
        rules = config["contrast"]
        require(isinstance(rules, list) and rules, "rules.json：对比度规则不能为空")
        ids = set()
        for rule in rules:
            required = {"id", "foreground", "background", "minimum", "description", "level"}
            require(isinstance(rule, dict) and required <= set(rule) <= required | {"canvas"}, "对比度规则字段不完整或未知")
            require(all(isinstance(rule[k], str) and rule[k] for k in required - {"minimum"}), "对比度规则文本字段不能为空")
            require(rule["level"] in LEVELS, f"{rule['id']}：level 只能是 {'、'.join(sorted(LEVELS))}")
            require(rule["id"] not in ids, f"规则 id 重复：{rule['id']}")
            ids.add(rule["id"])
            require(number(rule["minimum"]) and 1 <= rule["minimum"] <= 21, f"{rule['id']}：无效的对比度下限")
            for key in ("foreground", "background", "canvas"):
                if key not in rule:
                    continue
                require(isinstance(rule[key], str), f"{rule['id']}：{key} 必须是路径")
                path = rule[key].replace("{style}", "glass").replace("{mode}", "light")
                require(re.fullmatch(r"[a-zA-Z0-9]+(?:\.[a-zA-Z0-9]+)*", path), f"{rule['id']}：无效的路径模板")
        # Resolve every combination now, including rules that happen to pass later.
        for _, _, rule, paths in self.rule_cases():
            for path in paths.values():
                self.resolve(path)
                require(self.tokens[path]["$type"] == "color", f"{rule['id']}：{path} 不是颜色")

    def _validate_matrix(self):
        semantic_paths = [p.split(".") for p in self.tokens if p.startswith("semantic.")]
        require(all(len(p) >= 4 and p[1] in STYLES and p[2] in MODES for p in semantic_paths), "语义色包含未知风格或模式")
        status_paths = [p.split(".") for p in self.tokens if p.startswith("status.")]
        require(all(len(p) >= 3 and p[1] in MODES for p in status_paths), "状态色包含未知模式")
        for prefix, dimensions in (("semantic", [(s, m) for s in sorted(STYLES) for m in sorted(MODES)]), ("status", [(m,) for m in sorted(MODES)])):
            expected = None
            for parts in dimensions:
                start = ".".join((prefix, *parts)) + "."
                leaves = {p[len(start):]: t["$type"] for p, t in self.tokens.items() if p.startswith(start)}
                require(leaves, f"缺少 token 分组：{start}")
                if expected is None:
                    expected = leaves
                require(leaves == expected, f"风格或深浅色的 token 不齐：{start}")
        # Output names must remain unambiguous after case conversion and flattening.
        for naming in (css_name, kotlin_path):
            seen = set()
            for path in self.tokens:
                name = naming(path)
                require(name not in seen, f"生成名称冲突：{path} → {name}")
                seen.add(name)
        groups = {".".join(p.split(".")[:i]) for p in self.tokens for i in range(1, len(p.split(".")))}
        require(not groups.intersection(self.tokens), "同一路径不能既是 token 又是分组")

    def rule_cases(self):
        for style in sorted(self.rules["styles"]):
            for mode in sorted(self.rules["modes"]):
                for rule in self.rules["contrast"]:
                    paths = {k: rule[k].replace("{style}", style).replace("{mode}", mode) for k in ("foreground", "background", "canvas") if k in rule}
                    yield style, mode, rule, paths

    def contrast_results(self):
        results = []
        for style, mode, rule, paths in self.rule_cases():
            fg, bg = (self.values[paths[k]] for k in ("foreground", "background"))
            canvas = self.values[paths["canvas"]] if "canvas" in paths else None
            ratio = contrast_ratio(fg, bg, canvas)
            results.append(ContrastResult(style, mode, rule["id"], paths["foreground"], paths["background"], ratio, rule["minimum"], rule["level"]))
        return results


@dataclass(frozen=True)
class ContrastResult:
    style: str
    mode: str
    rule: str
    foreground: str
    background: str
    ratio: float
    minimum: float
    level: str = "error"

    @property
    def passed(self):
        return self.ratio >= self.minimum

    def message(self):
        return f"{self.style}/{self.mode} {self.rule}：{self.ratio:.4f}:1 < {self.minimum:g}:1（{self.foreground} / {self.background}）"


def composite(foreground, background):
    require(background["alpha"] == 1, "透明背景必须提供不透明画布")
    alpha = foreground["alpha"]
    return {"colorSpace": "srgb", "components": [f * alpha + b * (1 - alpha) for f, b in zip(foreground["components"], background["components"])], "alpha": 1}


def luminance(color):
    require(color["alpha"] == 1, "相对亮度计算前必须合成透明色")
    linear = [v / 12.92 if v <= 0.04045 else ((v + 0.055) / 1.055) ** 2.4 for v in color["components"]]
    return sum(v * w for v, w in zip(linear, (0.2126, 0.7152, 0.0722)))


def contrast_ratio(foreground, background, canvas=None):
    if background["alpha"] != 1:
        require(canvas is not None, "透明背景缺少 canvas，不能默认按白底检查")
        background = composite(background, canvas)
    foreground = composite(foreground, background)
    low, high = sorted((luminance(foreground), luminance(background)))
    return (high + 0.05) / (low + 0.05)


def fmt(value):
    return format(value, ".12g")


def css_name(path):
    return re.sub(r"([a-z0-9])([A-Z])", r"\1-\2", path.replace(".", "-")).lower()


def kotlin_identifier(name):
    return f"`{name}`" if name in KEYWORDS else name


def kotlin_path(path):
    parts = path.split(".")
    return ".".join([p[0].upper() + p[1:] for p in parts[:-1]] + [kotlin_identifier(parts[-1])])


def css_value(kind, value, path):
    if kind == "color":
        rgb = " ".join(fmt(v * 255) for v in value["components"])
        return f"rgb({rgb} / {fmt(value['alpha'])})"
    if kind in ("dimension", "duration"):
        # Font sizes use rem so browser text-size preferences remain effective.
        if path.startswith(("type.", "typeGlass.")):
            return fmt(value["value"] / 16) + "rem"
        return fmt(value["value"]) + value["unit"]
    if kind == "string":
        return json.dumps(value, ensure_ascii=False)
    return fmt(value)


def kotlin_value(kind, value, path):
    if kind == "color":
        channels = [*value["components"], value["alpha"]]
        # Float components retain alpha (e.g. 0.14), unlike packed 8-bit ARGB.
        return "Color", "Color(" + ", ".join(fmt(v) + "f" for v in channels) + ")"
    if kind == "dimension":
        unit = "sp" if path.startswith(("type.", "typeGlass.")) else "dp"
        return ("TextUnit" if unit == "sp" else "Dp"), fmt(value["value"]) + "." + unit
    if kind == "duration":
        return "Int", str(round(value["value"] * (1000 if value["unit"] == "s" else 1)))
    if kind == "fontWeight":
        return "FontWeight", f"FontWeight({value})"
    if kind == "string":
        return "String", json.dumps(value, ensure_ascii=False).replace("$", "\\$")
    return "Float", fmt(value) + "f"


def render_kotlin(catalog, package):
    require(re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*", package) and not any(p in KEYWORDS for p in package.split(".")), "Kotlin 包名无效")
    lines = ["// Generated by scripts/assets/gen_tokens.py; do not edit.", f"// Source SHA-256: {catalog.digest}", f"package {package}", "", "import androidx.compose.ui.graphics.Color", "import androidx.compose.ui.text.font.FontWeight", "import androidx.compose.ui.unit.Dp", "import androidx.compose.ui.unit.TextUnit", "import androidx.compose.ui.unit.dp", "import androidx.compose.ui.unit.sp", "", "object Tokens {"]
    tree = {}
    for path in sorted(catalog.tokens):
        node = tree
        parts = path.split(".")
        for part in parts[:-1]:
            node = node.setdefault(part, {})
        node[parts[-1]] = path

    def emit(tree, depth, scaled=False):
        indent = "    " * depth
        for key, child in sorted(tree.items()):
            if isinstance(child, dict):
                lines.append(f"{indent}object {key[0].upper() + key[1:]} {{")
                # spacing and corner radii follow Settings › Appearance › Layout density (LayoutDensity.kt); `Standard` keeps the values
                # for the few places that size something with a spacing token
                emit(child, depth + 1, scaled={"space": "spaceScale", "radius": "radiusScale"}.get(key) if depth == 1 else False)
                lines.append(indent + "}")
            elif scaled:
                kind, value = kotlin_value(catalog.tokens[child]["$type"], catalog.values[child], child)
                name = kotlin_identifier(key)
                lines.append(f"{indent}val {name}: {kind} get() = Standard.{name} * LayoutDensity.{scaled}")
            else:
                kind, value = kotlin_value(catalog.tokens[child]["$type"], catalog.values[child], child)
                modifier = "const val" if kind in {"Int", "Float", "String"} else "val"
                lines.append(f"{indent}{modifier} {kotlin_identifier(key)}: {kind} = {value}")
        if scaled:
            lines.append(f"{indent}object Standard {{")
            for key, child in sorted(tree.items()):
                kind, value = kotlin_value(catalog.tokens[child]["$type"], catalog.values[child], child)
                lines.append(f"{indent}    val {kotlin_identifier(key)}: {kind} = {value}")
            lines.append(indent + "}")
    emit(tree, 1)
    lines.append("}")
    return "\n".join(lines) + "\n"


def render_css(catalog):
    lines = ["/* Generated by scripts/assets/gen_tokens.py; do not edit.", f"   Source SHA-256: {catalog.digest} */", ":root {"]
    for path, token in sorted(catalog.tokens.items()):
        lines.append(f"  --{css_name(path)}: {css_value(token['$type'], catalog.values[path], path)};")
    lines.append("}")
    for style in sorted(STYLES):
        for mode in ("light", "dark"):
            selectors = [f'[data-style="{style}"][data-theme="{mode}"]']
            if mode == "light":
                selectors.append(f'[data-style="{style}"]:not([data-theme])')
            if style == "native":
                selectors.append(':root:not([data-style]):not([data-theme])' if mode == "light" else '[data-theme="dark"]:not([data-style])')
                if mode == "light":
                    selectors.append('[data-theme="light"]:not([data-style])')
            lines.append(",\n".join(selectors) + " {")
            for prefix, alias in ((f"semantic.{style}.{mode}.", ""), (f"common.{mode}.", ""), (f"status.{mode}.", "status-")):
                for path in sorted(catalog.tokens):
                    if path.startswith(prefix):
                        lines.append(f"  --{alias}{css_name(path[len(prefix):])}: var(--{css_name(path)});")
            lines.append("}")
    # Reduced motion: every motion duration becomes motion.reduced (zero).
    reduced = [path for path in sorted(catalog.tokens) if path.startswith("motion.") and path != "motion.reduced" and catalog.tokens[path]["$type"] == "duration"]
    if reduced and "motion.reduced" in catalog.tokens:
        lines.extend(["@media (prefers-reduced-motion: reduce) {", "  :root {", *(f"    --{css_name(path)}: var(--motion-reduced);" for path in reduced), "  }", "}"])
    return "\n".join(lines) + "\n"


def color_label(value):
    rgb = "#" + "".join(f"{round(v * 255):02X}" for v in value["components"])
    return rgb + (f" / α={fmt(value['alpha'])}" if value["alpha"] != 1 else "")


def md_escape(value):
    return value.replace("|", "\\|").replace("\n", " ").replace("\r", " ")


def render_md(catalog):
    lines = ["# 设计色板", "", f"<!-- Source SHA-256: {catalog.digest} -->", "", "由 `scripts/assets/gen_tokens.py` 生成。透明色保留 alpha，显示效果取决于承载背景。", "系统色为跨端回退值，未在设备上采样。", "", "| Token | sRGB / alpha | 来源 |", "| --- | --- | --- |"]
    for path, token in sorted(catalog.tokens.items()):
        if token["$type"] == "color":
            lines.append(f"| `{path}` | `{color_label(catalog.values[path])}` | {md_escape(token['$description'])} |")
    lines.extend(["", "## 对比度检查", "", "按 JSON 数值计算，先做透明度合成，再按 WCAG sRGB 相对亮度计算；不是设备采样。", "", "| 风格 | 模式 | 规则 | 实际 | 要求 | 结果 |", "| --- | --- | --- | --- | --- | --- |"])
    for result in catalog.contrast_results():
        lines.append(f"| {result.style} | {result.mode} | {md_escape(result.rule)} | {result.ratio:.4f} | {result.minimum:g} | {'通过' if result.passed else '不达标'} |")
    return "\n".join(lines) + "\n"


def main(argv=None):
    parser = argparse.ArgumentParser(description="生成和检查跨端设计 token")
    parser.add_argument("--source", type=Path, default=DEFAULT_SOURCE, help="token JSON 目录")
    parser.add_argument("--kotlin", type=Path, help="Tokens.kt 输出文件")
    parser.add_argument("--package", "--kotlin-package", default="persona.core.designsystem", help="Kotlin 包名")
    parser.add_argument("--css", type=Path, help="CSS 输出文件")
    parser.add_argument("--md", type=Path, help="色板 Markdown 输出文件")
    parser.add_argument("--check", action="store_true", help="只读校验结构、引用、对比度和指定产物")
    args = parser.parse_args(argv)
    if not args.check and not any((args.kotlin, args.css, args.md)):
        parser.error("请指定 --kotlin、--css、--md 或 --check")
    try:
        catalog = Catalog(args.source)
        outputs = []
        for path, render in ((args.kotlin, lambda: render_kotlin(catalog, args.package)), (args.css, lambda: render_css(catalog)), (args.md, lambda: render_md(catalog))):
            if path is not None:
                outputs.append((path, render().encode("utf-8")))
        targets = [p.resolve() for p, _ in outputs]
        require(len(set(targets)) == len(targets), "多个输出不能使用同一个路径")
        require(not any(p == args.source.resolve() / name for p in targets for name in catalog.documents), "输出不能覆盖 token JSON")
        results = catalog.contrast_results()
        failures = [r for r in results if not r.passed and r.level == "error" and (r.style, r.mode, r.rule) not in catalog.known_failures]
        known = [r for r in results if not r.passed and r.level == "error" and (r.style, r.mode, r.rule) in catalog.known_failures]
        advisory = [r for r in results if not r.passed and r.level == "warn"]
        failing = {(r.style, r.mode, r.rule) for r in results if not r.passed and r.level == "error"}
        fixed = sorted(catalog.known_failures - failing)
        for result in failures:
            print("对比度不达标：" + result.message(), file=sys.stderr)
        for style, mode, rule in fixed:
            print(f"{KNOWN_FAILURES} 里的 {style}/{mode} {rule} 已经达标（或规则不存在了），请从列表里删掉", file=sys.stderr)
        stale = []
        for path, content in outputs:
            if args.check:
                if not path.is_file() or path.read_bytes() != content:
                    stale.append(path)
                    print(f"生成文件缺失或过期：{path}", file=sys.stderr)
            else:
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(content)
        if args.check:
            print(f"检查完成：{len(catalog.tokens)} 个 token，{len(results)} 个对比度组合；底线不达标 {len(failures)} 项（另有已知例外 {len(known)} 项），"
                  f"建议项不达标 {len(advisory)} 项（不阻断），{len(stale)} 个产物不一致")
            return 1 if failures or fixed or stale else 0
        print(f"已生成 {len(outputs)} 个文件；底线不达标 {len(failures)} 项，建议项不达标 {len(advisory)} 项")
        return 0
    except (TokenError, OSError, RecursionError) as error:
        print(f"token 检查失败：{error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
