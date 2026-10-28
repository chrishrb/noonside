import type { DataSource, DataSourceKind, DataSourceConfig, DataSourceConfigWritable } from '@/api/types';
import type { Option } from '@/components/ui/form';
import i18n from '@/i18n';

/** Form values of the connection fields, keyed by field key. */
export type ConfigValues = Record<string, string | boolean>;

export interface KindField {
  key: keyof DataSourceConfigWritable & string;
  label: string;
  type?: 'text' | 'number' | 'secret' | 'bool' | 'multiline' | 'select';
  required?: boolean;
  def?: string | number | boolean;
  placeholder?: string;
  help?: string;
  span2?: boolean;
  min?: number;
  max?: number;
  options?: Option[];
  /** Only shown and sent when this returns true. */
  when?: (v: ConfigValues) => boolean;
}

export interface KindInfo {
  kind: string;
  label: string;
  transport: string;
  desc: string;
  /** Accepted by the API (DataSourceKind in api.yaml). */
  supported: boolean;
  /** in: brings readings into noonside (data sources tab); out: sends them elsewhere (integrations tab). */
  direction: 'in' | 'out';
  interval: number;
  intervalHelp?: string;
  fields: KindField[];
  summary: (c: DataSourceConfig, d?: DataSource) => [string, string];
  /** noonside provides the connection (hosted FTP): no connection fields. */
  hosted?: boolean;
  /** Adjusts the values after a change, e.g. to a protocol's default port. */
  adjust?: (prev: ConfigValues, next: ConfigValues) => ConfigValues;
}

export const visibleFields = (K: KindInfo, v: ConfigValues) => K.fields.filter((f) => !f.when || f.when(v));

/** The logger types, with labels in the active language (so call it at render time). */
export const kinds = (): KindInfo[] => {
  const t = i18n.t;
  const backfill: KindField = {
    key: 'backfill_history', label: t('sources.fields.backfill'), type: 'bool', span2: true, help: t('sources.fields.backfillHelp'),
  };
  const solarLogInterval = t('sources.kinds.solarlogInterval');
  return [
  {
    kind: 'solarlog_hosted', label: t('sources.kinds.hosted.label'), transport: t('sources.kinds.hosted.transport'), supported: true, direction: 'in', interval: 300,
    desc: t('sources.kinds.hosted.desc'),
    hosted: true,
    fields: [backfill],
    summary: (_, d) => [t('sources.hosted.account'), d?.upload ? `${d.upload.username}@${d.upload.host}` : '—'],
  },
  {
    kind: 'solarlog_remote', label: t('sources.kinds.remote.label'), transport: t('sources.kinds.remote.transport'), supported: true, direction: 'in', interval: 300,
    desc: t('sources.kinds.remote.desc'),
    intervalHelp: solarLogInterval,
    fields: [
      {
        key: 'protocol', label: t('sources.fields.protocol'), type: 'select', required: true, def: 'ftps',
        options: [
          { value: 'ftps', label: t('sources.fields.ftps') },
          { value: 'ftp', label: t('sources.fields.ftp') },
          { value: 'sftp', label: t('sources.fields.sftp') },
        ],
      },
      { key: 'host', label: t('sources.fields.host'), required: true, placeholder: 'ftp.example.net' },
      { key: 'port', label: t('sources.fields.port'), type: 'number', def: 21, min: 1, max: 65535 },
      { key: 'username', label: t('sources.fields.username') },
      { key: 'password', label: t('common.password'), type: 'secret' },
      { key: 'directory', label: t('sources.fields.remoteDir'), placeholder: t('sources.fields.homeDir'), help: t('sources.fields.remoteDirHelp') },
      {
        key: 'host_key', label: t('sources.fields.hostKey'), type: 'multiline', span2: true, placeholder: 'ssh-ed25519 AAAA…', when: (v) => v.protocol === 'sftp',
        help: t('sources.fields.hostKeyHelp'),
      },
      backfill,
    ],
    // Follow the protocol's default port unless the user picked another one.
    adjust: (prev, next) => {
      const port = (v: ConfigValues) => (v.protocol === 'sftp' ? '22' : '21');
      return !next.port || next.port === port(prev) ? { ...next, port: port(next) } : next;
    },
    summary: (c) => {
      const p = c.protocol ?? 'ftp';
      return [t('sources.summary.server'), `${p}://${c.host || '—'}:${c.port || (p === 'sftp' ? 22 : 21)}`];
    },
  },
  {
    kind: 'pvoutput_export', label: t('sources.kinds.pvoutput.label'), transport: t('sources.kinds.pvoutput.transport'), supported: true, direction: 'out', interval: 300,
    desc: t('sources.kinds.pvoutput.desc'),
    intervalHelp: t('sources.kinds.pvoutput.interval'),
    fields: [
      { key: 'pvoutput_system_id', label: t('sources.fields.pvoutputSystemId'), required: true, placeholder: '12345', help: t('sources.fields.pvoutputSystemIdHelp') },
      { key: 'api_key', label: t('sources.fields.pvoutputApiKey'), type: 'secret', required: true, help: t('sources.fields.pvoutputApiKeyHelp') },
    ],
    summary: (c) => [t('sources.fields.pvoutputSystemId'), c.pvoutput_system_id || '—'],
  },
  // Not implemented in the backend yet; shown so people know they are planned.
  { kind: 'fronius_solarapi', label: 'Fronius · Solar API', transport: t('sources.kinds.httpLocal'), supported: false, direction: 'in', interval: 60, desc: t('sources.kinds.fronius'), fields: [], summary: () => [t('sources.fields.host'), '—'] },
  { kind: 'sunspec_modbus', label: 'Modbus TCP · SunSpec', transport: t('sources.kinds.modbusLocal'), supported: false, direction: 'in', interval: 60, desc: t('sources.kinds.sunspec'), fields: [], summary: () => [t('sources.fields.host'), '—'] },
  { kind: 'solaredge_cloud', label: 'SolarEdge · Monitoring API', transport: t('sources.kinds.cloud'), supported: false, direction: 'in', interval: 900, desc: t('sources.kinds.solaredge'), fields: [], summary: () => [t('sources.summary.site'), '—'] },
  { kind: 'huawei_fusionsolar', label: 'Huawei · FusionSolar', transport: t('sources.kinds.cloud'), supported: false, direction: 'in', interval: 300, desc: t('sources.kinds.huawei'), fields: [], summary: () => [t('sources.summary.plant'), '—'] },
  ];
};

export const kindOf = (k: DataSourceKind | string) => {
  const all = kinds();
  return all.find((x) => x.kind === k) ?? all.find((x) => x.kind === 'solarlog_remote')!;
};
