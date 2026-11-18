"""MOCK_VOLC=1 (with MOCK_EXTENSIONS=1): the built-in Volcengine speech extension (docs/extensions/volcengine-speech.md),
switched off as on a new hub. Its form, its key check (a key with "bad" in it is refused as the service refuses a key;
"notts" in it, synthesis is not turned on), the meetings switch (the assistant's choice, never saved with the settings)
and its page (Connection and Test) work in memory with made-up outcomes. Nothing reaches a real service.
"""
import os

ENABLED = os.environ.get("MOCK_VOLC") == "1"
EXT = "volc"
REF = "volc/volc"

_VOICES = [("zh_female_vv_uranus_bigtts", "Vivi（女声，多语种）"), ("zh_female_xiaohe_uranus_bigtts", "小何（女声）"),
           ("zh_male_m191_uranus_bigtts", "云舟（男声）"), ("zh_male_taocheng_uranus_bigtts", "小天（男声）"),
           ("zh_female_qingxinnvsheng_uranus_bigtts", "清新女声"), ("zh_female_shuangkuaisisi_uranus_bigtts", "爽快思思（女声）")]
_MODELS = [("volc.seedasr.sauc.duration", "豆包 2.0，按时长计费（默认）"), ("volc.seedasr.sauc.concurrent", "豆包 2.0，按并发计费"),
           ("volc.bigasr.sauc.duration", "1.0，按时长计费"), ("volc.bigasr.sauc.concurrent", "1.0，按并发计费")]

FORM = [
    {"name": "meetings", "type": "boolean", "title": "用于会议转录", "default": False,
     "description": "新会议默认「在 Hub 上转录」，用火山引擎识别。和录音设置里的转录服务是同一个选择。",
     "confirm": "打开后，新会议默认在 Hub 上转录：录音会实时发给火山引擎识别，纪要写好后 Hub 删除音频。"},
    {"name": "read_answers", "type": "boolean", "title": "参与者回答用火山朗读", "default": True,
     "description": "会议设成「参与者」时，用下面的音色念 Persona 的回答；关掉或出错时用手机自带的声音。"},
    {"name": "voice", "type": "string", "title": "音色", "default": _VOICES[0][0], "enum": [{"value": v, "title": t} for v, t in _VOICES]},
    {"name": "advanced", "type": "object", "title": "高级", "fields": [
        {"name": "asr_model", "type": "string", "title": "识别模型", "description": "要在豆包语音控制台开通对应的模型。",
         "default": _MODELS[0][0], "enum": [{"value": v, "title": t} for v, t in _MODELS]},
        {"name": "voice_id", "type": "string", "title": "其他音色 ID", "pattern": "^[A-Za-z0-9_.-]{0,128}$",
         "description": "从豆包语音控制台「音色库」复制的音色 ID；填了就代替上面的音色。"},
        {"name": "app_id", "type": "string", "title": "App ID（旧版控制台）", "pattern": "^[A-Za-z0-9_-]{0,64}$",
         "description": "只在用旧版控制台的 App ID 和 Access Token 时填，这时上面的 API Key 栏填 Access Token。"}]},
]

STATE = {"asr": None, "tts": None}  # each service's last outcome: None (not tested), "ok" or a failure's words

WHY_FORBIDDEN = "火山引擎拒绝了这把 Key：到豆包语音控制台「API Key管理」重新复制一把，粘贴后保存。"
WHY_TTS = "这把 Key 的项目还没开通「豆包语音合成模型2.0」：到豆包语音控制台「开通管理」开通；不需要朗读就关掉「参与者回答用火山朗读」。"


def plugin(runs):
    """The extension's entry in the mock's PLUGINS (runs: the mock's line for a program run on the hub)."""
    return {"id": EXT, "name": "火山语音", "kind": "ui", "status": "disabled", "version": "202611160000", "source": "builtin",
            "enabled": False, "homepage": "https://www.volcengine.com/product/voice-tech",
            "description": "用你的火山引擎（豆包语音）账号做会议的 Hub 转录，并在会议设成「参与者」时把回答念出来。粘贴 API Key，打开「用于会议转录」就能用。",
            "contributes": {"transcription": ["volc"], "speech": ["volc"], "ui": ["status"]},
            "form": {"fields": FORM}, "config": {},
            "secrets": [{"name": "key", "title": "API Key", "set": False,
                         "description": "在豆包语音控制台「API Key管理」复制。保存时会测一次识别和合成。"}],
            "permissions": [runs, {"key": "network", "title": "访问网络", "detail": "openspeech.bytedance.com"}],
            "ui": [{"plugin": EXT, "plugin_name": "火山语音", "id": "status", "title": "连接与测试", "placement": "plugin_page", "size": "full"}]}


def key_set(pl):
    return any(s["name"] == "key" and s.get("set") for s in pl.get("secrets", []))


def view(pl, assistant):
    """The detail as the hub sends it: needs setup without a key, the switch as the assistant's choice is."""
    if pl["enabled"]:
        pl["status"] = "needs_setup" if not key_set(pl) else "running"
        pl["needs_setup"] = None if key_set(pl) else ["火山引擎的 API Key"]
    pl["config"] = {**{k: v for k, v in pl["config"].items() if k != "meetings"},
                    "meetings": assistant.get("meeting_transcription_extension") == REF}
    return pl


def config_put(pl, cfg, assistant):
    """PUT …/config: the switch sets the assistant's choice and is not saved; (error, code) or None."""
    cfg = dict(cfg)
    want = cfg.pop("meetings", None)
    if isinstance(want, bool) and want != (assistant.get("meeting_transcription_extension") == REF):
        if want:
            assistant["meeting_transcription_extension"], assistant["meeting_transcription_default"] = REF, "hub"
        elif assistant.get("meeting_transcription_extension") == REF:
            assistant["meeting_transcription_extension"], assistant["meeting_transcription_default"] = "", "device"
    pl["config"] = cfg
    return None


def secret_put(pl, value):
    """PUT …/secrets/key, checked first as settings.check does: the refusal's words, or None once saved."""
    if not pl["enabled"]:
        return "扩展没在运行，没法校验：先打开它再保存"
    if "bad" in value:
        return WHY_FORBIDDEN
    read = pl["config"].get("read_answers", True) is not False
    if "notts" in value and read:
        return "语音合成：" + WHY_TTS
    STATE["asr"], STATE["tts"] = "ok", ("ok" if read else None)
    for s in pl["secrets"]:
        if s["name"] == "key":
            s["set"], s["hint"] = True, value[-4:] if len(value) >= 12 else None
    return None


def _row(title, outcome, on=True):
    row = {"type": "row", "key": title, "title": title}
    if not on:
        row["value"] = "朗读已关闭"
    elif outcome is None:
        row["value"] = "还没测试"
    elif outcome == "ok":
        row.update(value="320 毫秒", badge={"text": "已连通", "tone": "positive"})
    else:
        row.update(subtitle=outcome, badge={"text": "没连通", "tone": "danger"})
    return row


def page(pl):
    """The extension's page (Connection and Test)."""
    console = {"type": "link", "key": "console", "title": "打开豆包语音控制台", "url": "https://console.volcengine.com/speech/new"}
    if not key_set(pl):
        return {"sections": [{"items": [
            {"type": "text", "key": "need_key", "text": "先在上面填 API Key 并保存，保存时会自动测一次。"},
            {"type": "text", "key": "steps", "style": "secondary",
             "text": "还没有 Key：在豆包语音控制台「开通管理」开通「豆包流式语音识别模型2.0」和「豆包语音合成模型2.0」，再到「API Key管理」复制 Key。"},
            console]}]}
    read = pl["config"].get("read_answers", True) is not False
    return {"sections": [
        {"key": "connection", "title": "连接", "footer": "测试会识别 0.6 秒静音、合成「你好」两个字，几乎不花钱。", "items": [
            _row("语音识别", STATE["asr"]), _row("语音合成", STATE["tts"], read),
            {"type": "button", "key": "test", "title": "测试连接", "action": {"id": "test"}}]},
        {"key": "help", "items": [console]}]}


def page_post(pl, req):
    """POST …/-/ui/status: the page, or Test Connection's answer."""
    if req.get("action") == "test" and key_set(pl):
        STATE["asr"] = "ok"
        if pl["config"].get("read_answers", True) is not False:
            STATE["tts"] = "ok"
        return {"view": page(pl), "toast": "识别和合成都通了"}
    return {"view": page(pl)}
