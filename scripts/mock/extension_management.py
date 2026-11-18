"""In-memory fixture enabled only with MOCK_EXTENSION_MANAGEMENT=1; never touches a production hub or installs real packages."""
import copy
import json
from datetime import datetime, timedelta, timezone
from urllib.parse import urlsplit, parse_qs


class ExtensionManagementMock:
    def __init__(self, plugins, catalog, market):
        self.plugins, self.catalog, self.market = plugins, catalog, market
        self.ids = ['sample-kit', 'sample-pack', 'sample-extra', 'sample-child', 'sample-unresolved']
        self.base = {
            'sample-kit': {'id': 'sample-kit', 'name': '示例工具包', 'kind': 'ui', 'status': 'running', 'enabled': True,
                'version': '1.0.0', 'source': 'installed', 'origin': 'market:' + market + '/sample-kit',
                'description': '贡献独立开关；提示和模板只填草稿。', 'permissions': [], 'secrets': [],
                'config': {'title': '初始标题', 'triage_rules': '原有规则'},
                'form': {'fields': [{'name': 'title', 'type': 'string', 'title': '标题'},
                                    {'name': 'triage_rules', 'type': 'string', 'title': '分拣规则'}]},
                'contributes': {'ui': ['home'], 'themes': ['blue', 'bad'], 'prompts': ['weekly'], 'triage_templates': ['focus']},
                'depends': [{'id': 'example-notes', 'name': '基础笔记扩展', 'state': 'ok', 'installed': '1.2.0'}],
                'contributions': [{'point': 'ui', 'id': 'home', 'title': '概况页面', 'enabled': True, 'can_disable': True},
                    {'point': 'themes', 'id': 'blue', 'title': '示例蓝', 'enabled': True, 'can_disable': True},
                    {'point': 'themes', 'id': 'bad', 'title': '低对比度主题', 'enabled': True, 'can_disable': True},
                    {'point': 'connectors', 'id': 'source', 'title': '连接器来源', 'enabled': True, 'can_disable': False, 'via': 'connector'}]},
            'sample-extra': {'id': 'sample-extra', 'name': '示例补充工具', 'kind': 'ui', 'status': 'running', 'enabled': True,
                'source': 'installed', 'version': '1.0.0', 'origin': 'market:' + market + '/sample-extra', 'config': {}, 'permissions': []},
            'sample-child': {'id': 'sample-child', 'name': '示例依赖者', 'kind': 'ui', 'status': 'running', 'enabled': True,
                'source': 'installed', 'version': '1.0.0', 'config': {}, 'permissions': [], 'contributes': {}},
            'sample-pack': {'id': 'sample-pack', 'name': '示例扩展合集', 'kind': 'extension_pack', 'status': 'running', 'enabled': True,
                'source': 'installed', 'version': '1.0.0', 'origin': 'market:' + market + '/sample-pack', 'config': {}, 'permissions': [],
                'extension_pack': [{'id': 'sample-kit'}, {'id': 'sample-extra'}], 'pack_members_follow': True,
                'pack_on': [{'id': 'sample-kit', 'name': '示例工具包'}, {'id': 'sample-extra', 'name': '示例补充工具'}]},
        }
        for eid, name in [('sample-pack', '示例扩展合集'), ('sample-kit', '示例工具包'), ('sample-unresolved', '缺少依赖的示例')]:
            catalog.append({'id': eid, 'name': name, 'version': '1.0.0', 'description': '仅用于扩展管理的回归测试。',
                'market': market, 'market_name': '示例市场', 'contributes': {}, 'permissions': [], 'publisher': 'example'})
        self.reset({})

    def reset(self, options):
        for eid in self.ids:
            self.plugins.pop(eid, None)
        self.plugins.update(copy.deepcopy(self.base))
        if options.get('uninstalled'):
            for eid in ['sample-pack', 'sample-kit', 'sample-extra']:
                self.plugins.pop(eid, None)
        self.writes = []
        self.theme_reads = 0
        self.detail_reads = 0
        self.previews = {}
        self.options = options

    def active(self, point, cid):
        kit = self.plugins.get('sample-kit')
        return bool(kit and kit['enabled'] and next((c['enabled'] for c in kit['contributions'] if c['point'] == point and c['id'] == cid), True))

    def sync_pack(self):
        pack = self.plugins.get('sample-pack')
        if not pack:
            return
        on = [{'id': ref['id'], 'name': self.plugins[ref['id']]['name']} for ref in pack['extension_pack']
              if self.plugins.get(ref['id'], {}).get('enabled')]
        if on:
            pack['pack_on'] = on
        else:
            pack.pop('pack_on', None)
        if not pack.get('pack_off'):
            pack.pop('pack_off', None)

    def forget_pack_member(self, member_id):
        pack = self.plugins.get('sample-pack')
        if pack and member_id in pack.get('pack_off', []):
            pack['pack_off'].remove(member_id)

    def route(self, handler, path, body):
        parsed = urlsplit(handler.path)
        q = parse_qs(parsed.query)
        method = handler.command
        request = json.loads(body or b'{}')
        if path == '/mock/extension-management':
            if method == 'POST':
                self.reset(request)
            self.sync_pack()
            handler.send_json({'writes': self.writes, 'theme_reads': self.theme_reads, 'detail_reads': self.detail_reads,
                'kit': self.plugins.get('sample-kit'), 'pack': self.plugins.get('sample-pack'),
                'extra': self.plugins.get('sample-extra'), 'child': self.plugins.get('sample-child')})
            return True
        if method == 'POST' and '/api/v1/' in path and '/send' in path:
            self.writes.append({'method': method, 'path': path})
        if not path.startswith('/api/v1/extensions/'):
            return False
        if path in ['/api/v1/extensions/-/prompts', '/api/v1/extensions/-/triage-templates']:
            template = path.endswith('triage-templates')
            active = self.active('triage_templates' if template else 'prompts', 'focus' if template else 'weekly')
            item = {'extension': 'sample-kit', 'extension_name': '示例工具包', 'id': 'focus' if template else 'weekly',
                'title': '重要事项优先' if template else '整理示例周报', 'text': '只保留需要行动的重要事项' if template else '整理本周的进展，先列需要我决定的事项。'}
            handler.send_json({'templates' if template else 'prompts': [item] if active else []})
            return True
        if path == '/api/v1/extensions/-/themes':
            self.theme_reads += 1
            themes = []
            for cid, name, light, dark in [('blue', '示例蓝', '#2359B6', '#4477CC'), ('bad', '低对比度主题', '#FFF59D', '#FFF59D')]:
                if self.active('themes', cid):
                    themes.append({'extension': 'sample-kit', 'extension_name': '示例工具包', 'id': cid, 'name': name,
                        'light': {'accent': light}, 'dark': {'accent': dark}})
            handler.send_json({'themes': themes})
            return True
        parts = path.split('/')
        eid = parts[4] if len(parts) > 4 else ''
        if method != 'GET' and (eid in self.ids or path in ['/api/v1/extensions/-/catalog/preview', '/api/v1/extensions/-/install']):
            self.writes.append({'method': method, 'path': path, 'body': request, 'query': q})
        if path == '/api/v1/extensions/-/catalog/preview' and request.get('extension') in self.ids:
            eid = request['extension']
            expires = datetime.now(timezone.utc) + timedelta(minutes=10)
            if self.options.get('expiresSoon'): expires = datetime.now(timezone.utc) + timedelta(seconds=20)
            if self.options.get('expired'): expires -= timedelta(days=1)
            def preview(eid, name, permissions):
                return {'preview_id': 'preview-' + eid, 'permissions_digest': 'digest-' + eid,
                    'manifest': {'id': eid, 'name': name, 'version': '1.0.0'}, 'permissions': permissions,
                    'expires_at': expires.strftime('%Y-%m-%dT%H:%M:%SZ')}
            pv = preview(eid, '示例扩展合集' if eid == 'sample-pack' else '缺少依赖的示例', [])
            if eid == 'sample-unresolved':
                pv['unresolved'] = ['missing-source']
            elif eid == 'sample-pack':
                pv['manifest']['extension_pack'] = self.base['sample-pack']['extension_pack']
                pv['bundle'] = [preview('sample-kit', '示例工具包', [{'key': 'network', 'title': '工具包访问示例网络'}]),
                    preview('sample-extra', '示例补充工具', [{'key': 'files', 'title': '补充工具读取示例文件'}])]
            self.previews[pv['preview_id']] = pv
            handler.send_json(pv)
            return True
        if path == '/api/v1/extensions/-/install' and request.get('preview_id') in self.previews:
            pv = self.previews[request['preview_id']]
            expected = [{'preview_id': p['preview_id'], 'permissions_digest': p['permissions_digest']} for p in pv.get('bundle', [])]
            if pv.get('unresolved') or request.get('permissions_digest') != pv['permissions_digest'] or request.get('bundle', []) != expected:
                handler.send_json({'error': '权限确认不完整', 'code': 'install_failed'}, 400)
                return True
            self.plugins.update(copy.deepcopy(self.base))
            handler.send_json(self.plugins[pv['manifest']['id']])
            return True
        if eid not in self.ids or len(parts) < 6 or parts[5] != '-':
            return False
        if eid not in self.plugins:
            handler.send_json({'error': '扩展不存在'}, 404)
            return True
        kit = self.plugins[eid]
        rest = parts[6:]
        if method == 'GET' and rest in [[], ['']]:
            if eid == 'sample-kit': self.detail_reads += 1
            if eid == 'sample-pack': self.sync_pack()
            handler.send_json(kit)
            return True
        if method == 'PUT' and rest == ['config']:
            kit['config'] = request['config']
        elif method == 'PUT' and len(rest) == 3 and rest[0] == 'contributions':
            contribution = next(c for c in kit['contributions'] if c['point'] == rest[1] and c['id'] == rest[2])
            if not contribution['can_disable']:
                handler.send_json({'error': '跟着连接器'}, 400)
                return True
            contribution['enabled'] = request['enabled']
        elif (method == 'PUT' and rest == ['enabled'] and not request.get('enabled')) or (method == 'DELETE' and rest in [[], ['']]):
            cascade = request.get('cascade') if method == 'PUT' else q.get('cascade') == ['1']
            if self.plugins.get('sample-child', {}).get('enabled') and not cascade:
                handler.send_json({'error': '需要先确认依赖者', 'code': 'has_dependents', 'dependents': [{'id': 'sample-child', 'name': '示例依赖者'}]}, 409)
                return True
            self.plugins['sample-child']['enabled'] = False
            self.plugins['sample-child']['status'] = 'disabled'
            if method == 'DELETE':
                self.plugins.pop(eid)
                removed = []
                if q.get('members') == ['1']:
                    # Keep one member, to check the client does not report a partial result as full success.
                    if self.plugins.pop('sample-kit', None): removed.append('sample-kit')
                handler.send_json({'ok': True, 'removed_members': removed})
                return True
            if eid == 'sample-pack':
                enabled_members = [ref['id'] for ref in kit['extension_pack'] if self.plugins.get(ref['id'], {}).get('enabled')]
                for member_id in enabled_members:
                    self.plugins[member_id]['enabled'] = False
                    self.plugins[member_id]['status'] = 'disabled'
                    if member_id not in kit.setdefault('pack_off', []):
                        kit['pack_off'].append(member_id)
            else:
                self.forget_pack_member(eid)
            kit['enabled'] = False
            kit['status'] = 'disabled'
        elif method == 'PUT' and rest == ['enabled']:
            kit['enabled'] = True
            kit['status'] = 'running'
            if eid == 'sample-pack':
                for member_id in kit.pop('pack_off', []):
                    if member_id in self.plugins:
                        self.plugins[member_id]['enabled'] = True
                        self.plugins[member_id]['status'] = 'running'
            else:
                self.forget_pack_member(eid)
        else:
            return False
        if eid == 'sample-pack': self.sync_pack()
        handler.send_json(kit)
        return True
