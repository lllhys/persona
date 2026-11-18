#!/usr/bin/env python3
"""TestFlight setup for Persona Side (idempotent): an internal group, and an external one for anybody else.

  python3 scripts/release/asc_testflight.py                    # group "Internal" + testers, then status
  python3 scripts/release/asc_testflight.py status             # the last few builds and their processing state
  python3 scripts/release/asc_testflight.py wait [minutes] [build]  # poll until that build (default: newest) is VALID, then attach it
  python3 scripts/release/asc_testflight.py info "<What to Test>"   # write "What to Test" on the newest build, attach it
  python3 scripts/release/asc_testflight.py external           # group "Friends": newest build, Beta App Review, public link
  python3 scripts/release/asc_testflight.py next-build         # the next build number (both platforms, docs/operations/release.md)
  python3 scripts/release/asc_testflight.py catch-up           # newest build not submitted yet (another was in review): Friends + review

Add --platform mac (anywhere) for the Mac app's builds: same App Store Connect record and groups, its own builds.
Without it every command means the iPhone builds, so a Mac build never gets the iPhone build's notes or vice versa.

"wait" and "info" also hand the build to the external group when the config has app_store.external, so every new
TestFlight build reaches the external testers too (after Beta App Review); a failure there never fails the release.
A build can't be submitted while another of its version is in review; "catch-up" submits it once that is over. Nor
while App Store Connect's limit on review submissions is used up (422 SUBMISSION_LIMIT_REACHED): that prints one line
and a later "catch-up" submits it. Dropped connections are tried again in scripts/release/asc.py (#4034).

Internal testers (app_store.testers in the config) must be users of the team in App Store Connect.
External testers join the group by its public link when app_store.external.public_link is true (at most
public_link_limit people; 0 or unset means no limit), and/or get an email invitation when listed in app_store.external.testers. Without the
public link only those emails can join. The first build of a version goes through Beta App Review, which needs the
contact in app_store.external.review (docs/operations/release.md).
Groups are found by name and kind, the oldest one when several share a name; only "setup" and "external" create
a missing group, because App Store Connect sometimes lists them incompletely (#945). The App ID comes from app_store.app_id in the config (docs/operations/config.md).
"""
from __future__ import annotations

import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from asc import STORE, APIError, app_id, get_all, request  # noqa: E402

APP_ID = app_id()
GROUP = "Internal"
TESTERS = STORE.get("testers", [])  # [{"email", "first_name"}]
FEEDBACK_EMAIL = STORE.get("feedback_email", "")
EXTERNAL = STORE.get("external", {})  # {"group", "public_link", "public_link_limit", "testers", "review", "privacy_url"}
EXTERNAL_GROUP = EXTERNAL.get("group", "Friends")
PLATFORMS = {"ios": "IOS", "mac": "MAC_OS"}
PLATFORM = "IOS"  # which platform's builds the commands look at (--platform)

BETA_INFO = {
    "en-US": "Persona Side drives the Claude Code and Codex sessions on your own computers from the iPhone, the Apple Watch and the Mac: "
             "watch every step stream in, answer questions, approve plans and tool calls. Its assistant can also read your "
             "mail and calendar and, when you ask, your contacts, reminders, photos and health data. It connects to a hub "
             "you run yourself (see the README of the project).",
    "zh-Hans": "Persona Side 让你在 iPhone、Apple Watch 和 Mac 上驱动自己各台电脑里的 Claude Code 和 Codex：实时看每一步、回答提问、"
               "审批计划和工具调用；助手还能读你的邮件和日历，在你需要时读写通讯录、提醒事项、照片和健康数据。"
               "需要连接你自己部署的 hub（见项目 README）。",
}


def default_whats_new() -> str:
    if PLATFORM == "MAC_OS":
        return ("请试试：用 iPhone 上的配对二维码或配对链接连上 hub，在侧边栏打开一个 Agent 对话，看每一步实时出现；"
                "遇到提问、计划和授权直接在 Mac 上处理，菜单栏图标能看到需要你的数量。有问题请在 TestFlight 里反馈。")
    return ("请试试：连上 hub 后新开一个会话，看 Claude 的每一步实时出现；遇到提问直接在手机上回答，遇到计划就审批；"
            "在「手机数据」里打开要允许的数据源，然后让 Claude 查一下通讯录或这周的日程。有问题请在 TestFlight 里反馈。")


# Only the commands that set groups up ("setup", "external") create a missing group; "wait", "info" and "catch-up"
# stop instead (#945).
CREATE_GROUPS = False


def find_group(name: str, internal: bool):
    """The app's beta group of this name and kind; the earliest created one when there are several.

    App Store Connect now and then answers the group listing with an empty or partial list, or times out. A group that
    exists then looks missing, and creating it on the spot made five groups called Friends (#945): the builds went into
    an empty group while the testers sat in the first one. So the listing is asked up to three times before the group
    counts as missing, and duplicates resolve to the oldest, which is the one people joined."""
    for attempt in range(3):
        if attempt:
            time.sleep(5)
        try:
            groups = get_all(f"/v1/apps/{APP_ID}/betaGroups")
        except (APIError, OSError) as e:  # a read timeout is an OSError
            print(f"listing beta groups failed ({e}); retrying")
            continue
        matches = [g for g in groups
                   if g["attributes"]["name"] == name and bool(g["attributes"].get("isInternalGroup")) == internal]
        if matches:
            return min(matches, key=lambda g: g["attributes"].get("createdDate") or "")
    return None


class GroupMissing(APIError):
    def __init__(self, name: str):
        super().__init__(404, f"no beta group {name!r} in the listing (App Store Connect sometimes returns an incomplete "
                              "one); not creating it here. Retry, or create it with "
                              "python3 scripts/release/asc_testflight.py setup / external")


def group_id() -> str:
    g = find_group(GROUP, internal=True)
    if g:
        return g["id"]
    if not CREATE_GROUPS:
        raise GroupMissing(GROUP)
    print("create group", GROUP)
    body = {"data": {"type": "betaGroups",
                     "attributes": {"name": GROUP, "isInternalGroup": True, "hasAccessToAllBuilds": True},
                     "relationships": {"app": {"data": {"type": "apps", "id": APP_ID}}}}}
    return request("POST", "/v1/betaGroups", body)["data"]["id"]


def add_tester(gid: str, email: str, first_name: str, last_name: str = ""):
    members = get_all(f"/v1/betaGroups/{gid}/betaTesters")
    if any(m["attributes"].get("email", "").lower() == email.lower() for m in members):
        print("already in group:", email)
        return
    # One email has a tester record per app, so linking another app's record answer with 409
    # "Tester(s) cannot be assigned to this group because they belong to another app". Creating the
    # tester together with the group relationship works and reuses the person.
    attrs = {"email": email, "firstName": first_name}
    if last_name:
        attrs["lastName"] = last_name
    body = {"data": {"type": "betaTesters", "attributes": attrs,
                     "relationships": {"betaGroups": {"data": [{"type": "betaGroups", "id": gid}]}}}}
    try:
        request("POST", "/v1/betaTesters", body)
        print("invited", email)
    except APIError as e:
        if e.status == 409:
            print(f"{email}: already a tester under another app (409); add it to this app's group by hand "
                  "in App Store Connect › TestFlight")
            return
        raise


def builds(limit: int = 5, platform: str | None = None):
    """The newest builds of one platform (PLATFORM unless given); platform "" means both."""
    params = {"filter[app]": APP_ID, "sort": "-uploadedDate", "limit": limit}
    platform = PLATFORM if platform is None else platform
    if platform:
        params["filter[preReleaseVersion.platform]"] = platform
    return request("GET", "/v1/builds", params=params)["data"]


def next_build() -> int:
    """The next build number: one more than the largest on either platform, so a number never means two commits.

    The Mac build of a release reuses that release's iPhone number instead (docs/operations/release.md "Build numbers")."""
    return max((int(b["attributes"]["version"]) for b in builds(50, platform="")), default=0) + 1


def status():
    found = builds()
    if not found:
        print("no", PLATFORM, "builds yet (./scripts/release/release.sh)")
        return
    for b in found:
        a = b["attributes"]
        print(PLATFORM, a["version"], a["processingState"], a.get("uploadedDate", "")[:16],
              "expired" if a.get("expired") else "", a.get("expirationDate", ""), b["id"])


def newest_valid():
    found = request("GET", "/v1/builds", params={"filter[app]": APP_ID, "filter[processingState]": "VALID",
                                                "filter[preReleaseVersion.platform]": PLATFORM,
                                                "sort": "-uploadedDate", "limit": 1})["data"]
    return found[0] if found else None


def attach(build, gid: str):
    """Put the build in the internal group so testers get it (internal groups are auto-approved).

    Groups with hasAccessToAllBuilds already see every build and reject explicit assignment."""
    g = request("GET", f"/v1/betaGroups/{gid}")
    if g["data"]["attributes"].get("hasAccessToAllBuilds"):
        print("build", build["attributes"]["version"], "is available to", GROUP, "(all builds)")
        return
    request("POST", f"/v1/betaGroups/{gid}/relationships/builds",
            {"data": [{"type": "builds", "id": build["id"]}]})
    print("build", build["attributes"]["version"], "is now in", GROUP)


def write_info(build, whats_new: str):
    """Test information at the app level and "What to Test" on one build."""
    locales = {l["attributes"]["locale"]: l for l in get_all(f"/v1/apps/{APP_ID}/betaAppLocalizations")}
    for locale, description in BETA_INFO.items():
        attrs = {"description": description, "feedbackEmail": FEEDBACK_EMAIL}
        if EXTERNAL.get("privacy_url"):
            attrs["privacyPolicyUrl"] = EXTERNAL["privacy_url"]  # external testing requires one
        if locale in locales:
            request("PATCH", f"/v1/betaAppLocalizations/{locales[locale]['id']}",
                    {"data": {"type": "betaAppLocalizations", "id": locales[locale]["id"], "attributes": attrs}})
        else:
            request("POST", "/v1/betaAppLocalizations",
                    {"data": {"type": "betaAppLocalizations",
                              "attributes": dict(attrs, locale=locale),
                              "relationships": {"app": {"data": {"type": "apps", "id": APP_ID}}}}})
    have = {l["attributes"]["locale"]: l for l in request("GET", f"/v1/builds/{build['id']}/betaBuildLocalizations")["data"]}
    for locale in BETA_INFO:
        if locale in have:
            request("PATCH", f"/v1/betaBuildLocalizations/{have[locale]['id']}",
                    {"data": {"type": "betaBuildLocalizations", "id": have[locale]["id"],
                              "attributes": {"whatsNew": whats_new}}})
        else:
            request("POST", "/v1/betaBuildLocalizations",
                    {"data": {"type": "betaBuildLocalizations",
                              "attributes": {"locale": locale, "whatsNew": whats_new},
                              "relationships": {"build": {"data": {"type": "builds", "id": build["id"]}}}}})
    print("what to test written on build", build["attributes"]["version"])


def build_by_version(version: str):
    found = request("GET", "/v1/builds", params={"filter[app]": APP_ID, "filter[version]": version,
                                                "filter[preReleaseVersion.platform]": PLATFORM, "limit": 1})["data"]
    return found[0] if found else None


def wait(minutes: int, version: str | None = None):
    """Poll until the build finishes processing, then attach it to the internal group.

    Pass the build number release.sh printed: right after an upload the new build is not listed yet, and "the newest
    build" is still the previous one (already VALID), which got attached instead of the new one."""
    deadline = time.time() + minutes * 60
    last = None
    while True:
        build = build_by_version(version) if version else (builds(1) or [None])[0]
        if build:
            a = build["attributes"]
            state = a["processingState"]
            if state != last:
                print(time.strftime("%H:%M:%S"), "build", a["version"], state)
                last = state
            if state == "VALID":
                write_info(build, default_whats_new())
                attach(build, group_id())
                share_external(build)
                return
            if state in ("INVALID", "FAILED"):
                sys.exit(f"build {a['version']} finished as {state}: check App Store Connect for the reason")
        if time.time() > deadline:
            sys.exit(f"still {last or 'not listed'} after {minutes} min (builds appear 10–30 min after upload)")
        time.sleep(30)


def external_group_id() -> str:
    """The external group, its public link on or off as the config says."""
    g = find_group(EXTERNAL_GROUP, internal=False)
    if g:
        public_link(g)
        return g["id"]
    if not CREATE_GROUPS:
        raise GroupMissing(EXTERNAL_GROUP)
    print("create group", EXTERNAL_GROUP)
    body = {"data": {"type": "betaGroups",
                     "attributes": {"name": EXTERNAL_GROUP, "isInternalGroup": False, "publicLinkEnabled": False,
                                    "feedbackEnabled": True},
                     "relationships": {"app": {"data": {"type": "apps", "id": APP_ID}}}}}
    g = request("POST", "/v1/betaGroups", body)["data"]
    public_link(g)
    return g["id"]


def public_link(g):
    """Turn the group's public link on (app_store.external.public_link, capped at public_link_limit people, 0 = no limit) or off.

    Anyone who has the link can install, but only once a build of the group has passed Beta App Review."""
    want = bool(EXTERNAL.get("public_link"))
    limit = int(EXTERNAL.get("public_link_limit") or 0)
    attrs = {"publicLinkEnabled": want}
    if want:
        attrs["publicLinkLimitEnabled"] = limit > 0
        if limit:
            attrs["publicLinkLimit"] = limit
    if any(g["attributes"].get(k) != v for k, v in attrs.items()):
        g = request("PATCH", f"/v1/betaGroups/{g['id']}",
                    {"data": {"type": "betaGroups", "id": g["id"], "attributes": attrs}})["data"]
        print("public link of", EXTERNAL_GROUP, "turned", "on" if want else "off",
              (f"(at most {limit} testers)" if limit else "(no tester limit)") if want else "")
    if want and g["attributes"].get("publicLink"):
        print("public link:", g["attributes"]["publicLink"])


def write_review_details() -> bool:
    """Beta App Review's contact and notes; False when the config has no contact yet (review can't be submitted)."""
    r = EXTERNAL.get("review", {})
    missing = [k for k in ("first_name", "last_name", "phone", "email") if not r.get(k)]
    if missing:
        print("app_store.external.review is missing", ", ".join(missing), "- not submitting for Beta App Review")
        return False
    attrs = {"contactFirstName": r["first_name"], "contactLastName": r["last_name"], "contactPhone": r["phone"],
             "contactEmail": r["email"], "notes": r.get("notes", ""), "demoAccountRequired": bool(r.get("demo_account"))}
    if r.get("demo_account"):
        attrs["demoAccountName"] = r["demo_account"].get("name", "")
        attrs["demoAccountPassword"] = r["demo_account"].get("password", "")
    detail = request("GET", f"/v1/apps/{APP_ID}/betaAppReviewDetail")["data"]
    request("PATCH", f"/v1/betaAppReviewDetails/{detail['id']}",
            {"data": {"type": "betaAppReviewDetails", "id": detail["id"], "attributes": attrs}})
    print("beta app review contact written")
    return True


# A build in one of these has been submitted for Beta App Review (or got through it).
SUBMITTED = ("WAITING_FOR_BETA_REVIEW", "IN_BETA_REVIEW", "BETA_APPROVED", "IN_BETA_TESTING")


def external_state(build) -> str | None:
    return request("GET", f"/v1/builds/{build['id']}/buildBetaDetail")["data"]["attributes"].get("externalBuildState")


def submit_for_review(build) -> bool:
    """Asks for Beta App Review; a build already submitted (or approved) answers 409/422, which is fine.

    False when App Store Connect's limit on review submissions is used up (422 SUBMISSION_LIMIT_REACHED): asking again
    at once only meets the limit again, so the build waits in the group and a later catch-up round submits it (#4034)."""
    version = build["attributes"]["version"]
    state = external_state(build)
    if state in SUBMITTED:
        print("build", version, "beta review:", state)
        return True
    try:
        request("POST", "/v1/betaAppReviewSubmissions",
                {"data": {"type": "betaAppReviewSubmissions",
                          "relationships": {"build": {"data": {"type": "builds", "id": build["id"]}}}}})
    except APIError as e:
        if e.status == 422 and "SUBMISSION_LIMIT_REACHED" in str(e.body):
            print(PLATFORM, "build", version, "not submitted for Beta App Review yet: the submission limit is used up "
                  "for now; a later catch-up round submits it")
            return False
        raise
    print("build", version, "submitted for Beta App Review")
    return True


def share(build):
    """Put the build in the external group and ask for Beta App Review; invite any listed testers once it is submitted.

    Without a review contact the build only sits in the group and nobody is added: testers could not install
    it before the review anyway."""
    gid = external_group_id()
    linked = {b["id"] for b in get_all(f"/v1/betaGroups/{gid}/builds")}
    if build["id"] not in linked:
        request("POST", f"/v1/betaGroups/{gid}/relationships/builds", {"data": [{"type": "builds", "id": build["id"]}]})
        print("build", build["attributes"]["version"], "is now in", EXTERNAL_GROUP)
    if not write_review_details():
        print("testers are invited once a build is submitted")
        return
    if not submit_for_review(build):  # the submission limit: testers wait for the build that gets submitted
        return
    testers = EXTERNAL.get("testers", [])
    if not testers and not EXTERNAL.get("public_link"):
        print("no app_store.external.testers and no public link in the config: nobody can join yet")
    for t in testers:
        add_tester(gid, t["email"], t.get("first_name") or t["email"].split("@")[0], t.get("last_name", ""))


def share_external(build):
    """share() after a release, when the config has an external group; its errors don't fail the release."""
    if not EXTERNAL:
        return
    try:
        share(build)
    except APIError as e:
        print(f"external group {EXTERNAL_GROUP}: {e} (retry: python3 scripts/release/asc_testflight.py external)")


def external(build=None):
    build = build or newest_valid()
    if not build:
        sys.exit("no VALID build yet (python3 scripts/release/asc_testflight.py wait)")
    write_info(build, default_whats_new())
    share(build)


def catch_up():
    """The newest build to the external group and Beta App Review if that has not happened yet, its "What to Test" as
    it is. A build of a version that has another one in review can't be submitted (422 ANOTHER_BUILD_IN_REVIEW: "submit
    it again once it gets completed"), and nothing else comes back for it: Mac 111–127 sat in Friends unsubmitted
    behind 110 (#792). Run every round by the local auto-tf, so the newest build goes once the review is over."""
    if not EXTERNAL:
        return
    build = newest_valid()
    if not build:
        print("no VALID", PLATFORM, "build yet")
        return
    version = build["attributes"]["version"]
    state = external_state(build)
    if state != "READY_FOR_BETA_SUBMISSION":  # submitted, through, rejected (by hand from here), or still processing
        print(PLATFORM, "build", version, "external:", state)
        return
    try:
        share(build)
    except APIError as e:
        if "ANOTHER_BUILD_IN_REVIEW" not in str(e):
            raise
        print(PLATFORM, "build", version, "waits: another build of its version is in Beta App Review")


def main():
    global PLATFORM, CREATE_GROUPS
    args = sys.argv[1:]
    if "--platform" in args:
        i = args.index("--platform")
        name = args[i + 1] if i + 1 < len(args) else ""
        if name not in PLATFORMS:
            sys.exit(f"--platform {name!r}: one of {', '.join(PLATFORMS)}")
        PLATFORM = PLATFORMS[name]
        del args[i:i + 2]
    if args[:1] == ["next-build"]:
        print(next_build())
    elif args[:1] == ["status"]:
        status()
    elif args[:1] == ["wait"]:
        wait(int(args[1]) if len(args) > 1 else 45, args[2] if len(args) > 2 else None)
    elif args[:1] == ["info"]:
        build = newest_valid()
        if not build:
            sys.exit("no VALID build yet (python3 scripts/release/asc_testflight.py wait)")
        write_info(build, args[1] if len(args) > 1 else default_whats_new())
        attach(build, group_id())
        share_external(build)
    elif args[:1] == ["external"]:
        CREATE_GROUPS = True
        external()
    elif args[:1] == ["catch-up"]:
        catch_up()
    elif args[:1] in ([], ["setup"]):
        CREATE_GROUPS = True
        gid = group_id()
        for t in TESTERS:
            add_tester(gid, t["email"], t.get("first_name") or t["email"].split("@")[0])
        status()
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    try:
        main()
    except APIError as e:
        sys.exit(str(e))
