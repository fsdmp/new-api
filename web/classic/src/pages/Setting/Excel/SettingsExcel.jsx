import React, { useEffect, useState, useRef } from 'react';
import {
  Banner,
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  Row,
  Select,
  Spin,
  Switch,
  TextArea,
} from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

function parseModels(json) {
  if (!json || json === '[]' || json === 'null') return [];
  try {
    const parsed = JSON.parse(json);
    if (!Array.isArray(parsed)) return [];
    return parsed;
  } catch {
    return [];
  }
}

export default function SettingsExcel(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    'excel_tmp_key.enabled': false,
    'excel_tmp_key.account': '',
    'excel_tmp_key.expire_days': 7,
    'excel_tmp_key.quota': 500000,
    'excel_version_check.minimum_versions': '{}',
    'excel_model_list.models': '[]',
    'excel_tools.mode': 'strip',
    'excel_tools.debug_endpoints': false,
    'excel_tools.log_body_max_bytes': 2048,
    'excel_tools.search': '',
    'excel_tools.fetch': '',
    'excel_tools.sandbox': '',
    'excel_tools.loop': '',
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  // Separate state for the editable model list
  const [models, setModels] = useState([]);

  // Sync models from inputs when they change
  useEffect(() => {
    setModels(parseModels(inputs['excel_model_list.models']));
  }, [inputs['excel_model_list.models']]);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function syncModelsToInputs(newModels) {
    setModels(newModels);
    setInputs((prev) => ({
      ...prev,
      'excel_model_list.models': JSON.stringify(newModels),
    }));
  }

  function addModel() {
    syncModelsToInputs([
      ...models,
      { id: '', display_name: '', target_model: '', enabled: true },
    ]);
  }

  function removeModel(index) {
    const next = [...models];
    next.splice(index, 1);
    syncModelsToInputs(next);
  }

  function moveUp(index) {
    if (index === 0) return;
    const next = [...models];
    [next[index - 1], next[index]] = [next[index], next[index - 1]];
    syncModelsToInputs(next);
  }

  function moveDown(index) {
    if (index === models.length - 1) return;
    const next = [...models];
    [next[index], next[index + 1]] = [next[index + 1], next[index]];
    syncModelsToInputs(next);
  }

  function updateModelField(index, field, value) {
    const next = [...models];
    next[index] = { ...next[index], [field]: value };
    syncModelsToInputs(next);
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = String(inputs[item.key]);
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        if (typeof inputs[key] === 'boolean') {
          currentInputs[key] =
            props.options[key] === 'true' || props.options[key] === true;
        } else if (typeof inputs[key] === 'number') {
          currentInputs[key] = parseInt(props.options[key]) || inputs[key];
        } else {
          currentInputs[key] = props.options[key];
        }
      }
    }
    const mergedInputs = { ...inputs, ...currentInputs };
    setInputs(mergedInputs);
    setInputsRow(mergedInputs);
    if (refForm.current) {
      refForm.current.setValues(mergedInputs);
    }
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
        >
          <Form.Section text={t('Excel 临时令牌')}>
            <Banner
              type='info'
              description={t(
                '允许未认证用户通过 Excel 插件创建临时 API 令牌',
              )}
              style={{ marginBottom: 16 }}
            />
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'excel_tmp_key.enabled'}
                  label={t('启用临时令牌')}
                  extraText={t('允许 Excel 插件创建临时 API 令牌')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('excel_tmp_key.enabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'excel_tmp_key.account'}
                  label={t('公共账户')}
                  extraText={t('拥有临时令牌的账户用户名')}
                  placeholder={t('输入用户名')}
                  onChange={handleFieldChange('excel_tmp_key.account')}
                  showClear
                  disabled={!inputs['excel_tmp_key.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'excel_tmp_key.expire_days'}
                  label={t('过期天数')}
                  extraText={t('临时令牌过期天数')}
                  min={1}
                  onChange={handleFieldChange('excel_tmp_key.expire_days')}
                  disabled={!inputs['excel_tmp_key.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'excel_tmp_key.quota'}
                  label={t('令牌额度')}
                  extraText={t('每个临时令牌的额度（内部单位）')}
                  min={0}
                  onChange={handleFieldChange('excel_tmp_key.quota')}
                  disabled={!inputs['excel_tmp_key.enabled']}
                />
              </Col>
            </Row>
          </Form.Section>

          <Form.Section text={t('Excel 模型列表')}>
            <Banner
              type='info'
              description={t(
                '配置 Excel 模型接口返回的模型列表，支持调整顺序和启用/禁用。留空则使用环境变量默认配置。',
              )}
              style={{ marginBottom: 16 }}
            />
            {models.map((model, index) => (
              <div
                key={index}
                style={{
                  border: '1px solid var(--semi-color-border)',
                  borderRadius: 8,
                  padding: 16,
                  marginBottom: 12,
                }}
              >
                <Row gutter={16}>
                  <Col xs={24} sm={12} md={7}>
                    <div className='semi-form-field'>
                      <label className='semi-form-field-label'>
                        {t('模型 ID')}
                      </label>
                      <Input
                        value={model.id}
                        onChange={(val) =>
                          updateModelField(index, 'id', val)
                        }
                        placeholder='e.g. claude-sonnet-4-6'
                        showClear
                      />
                    </div>
                  </Col>
                  <Col xs={24} sm={12} md={7}>
                    <div className='semi-form-field'>
                      <label className='semi-form-field-label'>
                        {t('显示名称')}
                      </label>
                      <Input
                        value={model.display_name}
                        onChange={(val) =>
                          updateModelField(index, 'display_name', val)
                        }
                        placeholder='e.g. Claude Sonnet'
                        showClear
                      />
                    </div>
                  </Col>
                  <Col xs={24} sm={12} md={7}>
                    <div className='semi-form-field'>
                      <label className='semi-form-field-label'>
                        {t('目标模型')}
                      </label>
                      <Input
                        value={model.target_model}
                        onChange={(val) =>
                          updateModelField(index, 'target_model', val)
                        }
                        placeholder={t('留空则使用模型 ID')}
                        showClear
                      />
                    </div>
                  </Col>
                  <Col
                    xs={24}
                    sm={24}
                    md={3}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'flex-end',
                      gap: 4,
                      paddingTop: 22,
                    }}
                  >
                    <Switch
                      checked={model.enabled}
                      onChange={(val) =>
                        updateModelField(index, 'enabled', val)
                      }
                      size='small'
                      checkedText={t('启')}
                      uncheckedText={t('禁')}
                    />
                    <Button
                      size='small'
                      disabled={index === 0}
                      onClick={() => moveUp(index)}
                    >
                      ↑
                    </Button>
                    <Button
                      size='small'
                      disabled={index === models.length - 1}
                      onClick={() => moveDown(index)}
                    >
                      ↓
                    </Button>
                    <Button
                      size='small'
                      type='danger'
                      onClick={() => removeModel(index)}
                    >
                      ×
                    </Button>
                  </Col>
                </Row>
              </div>
            ))}
            <Button
              theme='light'
              onClick={addModel}
              style={{ marginBottom: 12 }}
            >
              + {t('添加模型')}
            </Button>
          </Form.Section>

          <Form.Section text={t('Excel 版本检查')}>
            <Banner
              type='info'
              description={t(
                '配置客户端最低版本要求，低于该版本的客户端将收到更新提示。格式为 JSON，例如 {"excel-plugin":"1.2.0","ai-sdk":"2.0.0"}',
              )}
              style={{ marginBottom: 16 }}
            />
            <Row gutter={16}>
              <Col span={24}>
                <Form.TextArea
                  field={'excel_version_check.minimum_versions'}
                  label={t('最低版本配置')}
                  placeholder={'{"excel-plugin":"1.2.0","ai-sdk":"2.0.0"}'}
                  rows={4}
                  onChange={handleFieldChange(
                    'excel_version_check.minimum_versions',
                  )}
                />
              </Col>
            </Row>
          </Form.Section>

          <Form.Section text={t('Excel Server Tools（联网/抓取/沙箱）')}>
            <Banner
              type='info'
              description={t(
                'web_search / web_fetch / code_execution 三项 server tool 的网关内执行配置。模式：off=发布前行为，strip=剥离工具声明（安全默认），full=注入真实 schema 并在网关内执行。修改保存后约 1 分钟内自动生效，无需重启。',
              )}
              style={{ marginBottom: 16 }}
            />
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8}>
                <Form.Select
                  field={'excel_tools.mode'}
                  label={t('模式')}
                  extraText={t(
                    'full 生效前提：搜索已配置 api_keys，沙箱已部署（见下方 sandbox 配置）',
                  )}
                  size='default'
                  onChange={handleFieldChange('excel_tools.mode')}
                >
                  <Select.Option value='off'>
                    off（回滚到发布前行为）
                  </Select.Option>
                  <Select.Option value='strip'>
                    strip（剥离工具，安全默认）
                  </Select.Option>
                  <Select.Option value='full'>full（完整能力）</Select.Option>
                </Form.Select>
              </Col>
              <Col xs={24} sm={12} md={8}>
                <Form.Switch
                  field={'excel_tools.debug_endpoints'}
                  label={t('调试端点')}
                  extraText={t(
                    '开启后 POST /excel/api/tools/{web_search|web_fetch|code_execution} 可直连执行单个工具（联调用）',
                  )}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('excel_tools.debug_endpoints')}
                />
              </Col>
              <Col xs={24} sm={12} md={8}>
                <Form.InputNumber
                  field={'excel_tools.log_body_max_bytes'}
                  label={t('请求体日志截断')}
                  extraText={t('字节；0=不截断，负数=关闭请求体日志')}
                  min={-1}
                  onChange={handleFieldChange(
                    'excel_tools.log_body_max_bytes',
                  )}
                />
              </Col>
            </Row>
            <Row gutter={16}>
              <Col xs={24} lg={12}>
                <Form.TextArea
                  field={'excel_tools.search'}
                  label={t('搜索配置（JSON）')}
                  placeholder={
                    '{"provider":"zhipu","api_keys":{"zhipu":"key"},"max_results":5,"daily_quota":50}'
                  }
                  rows={5}
                  onChange={handleFieldChange('excel_tools.search')}
                />
              </Col>
              <Col xs={24} lg={12}>
                <Form.TextArea
                  field={'excel_tools.fetch'}
                  label={t('抓取配置（JSON）')}
                  placeholder={
                    '{"timeout_sec":15,"max_body_bytes":5242880,"daily_quota":100}'
                  }
                  rows={5}
                  onChange={handleFieldChange('excel_tools.fetch')}
                />
              </Col>
              <Col xs={24} lg={12}>
                <Form.TextArea
                  field={'excel_tools.sandbox'}
                  label={t('沙箱配置（JSON）')}
                  placeholder={
                    '{"enabled":false,"nsjail_path":"","rootfs_path":"","timeout_sec":30,"max_concurrent":4,"daily_quota":50}'
                  }
                  rows={5}
                  onChange={handleFieldChange('excel_tools.sandbox')}
                />
              </Col>
              <Col xs={24} lg={12}>
                <Form.TextArea
                  field={'excel_tools.loop'}
                  label={t('工具循环配置（JSON）')}
                  placeholder={'{"max_iterations":5,"ping_interval_sec":2}'}
                  rows={5}
                  onChange={handleFieldChange('excel_tools.loop')}
                />
              </Col>
            </Row>
          </Form.Section>

          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存 Excel 设置')}
            </Button>
          </Row>
        </Form>
      </Spin>
    </>
  );
}
