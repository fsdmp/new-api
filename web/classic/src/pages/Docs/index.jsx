import React, { useState, useMemo, useEffect } from 'react';
import { useParams, useNavigate, Link, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Nav,
  Input,
  Button,
  Layout,
  Spin,
  Empty,
} from '@douyinfe/semi-ui';
import { IllustrationConstruction } from '@douyinfe/semi-illustrations';
import {
  IconSearch,
  IconMenu,
  IconChevronDown,
} from '@douyinfe/semi-icons';
import { useIsMobile } from '@/hooks/common/useIsMobile';
import { MarkdownContent } from '@/components/common/markdown/MarkdownRenderer';
import '@/components/common/markdown/markdown.css';

// --- Navigation Config ---
const docsNavCategories = [
  {
    id: 'quick-start',
    titleKey: '快速开始',
    icon: 'rocket',
    items: [
      { slug: 'introduction', titleKey: '简介' },
      { slug: 'installation', titleKey: '安装部署' },
      { slug: 'configuration', titleKey: '配置' },
    ],
  },
  {
    id: 'download',
    titleKey: '下载',
    icon: 'download',
    items: [
      { slug: 'aiexcel', titleKey: 'AI Excel' },
      { slug: 'browser-extension', titleKey: '浏览器扩展' },
    ],
  },
  {
    id: 'api-guide',
    titleKey: 'API 指南',
    icon: 'book',
    items: [
      { slug: 'authentication', titleKey: '认证' },
      { slug: 'chat-completions', titleKey: '聊天补全' },
      { slug: 'models', titleKey: '模型' },
      { slug: 'rate-limits', titleKey: '速率限制' },
    ],
  },
  {
    id: 'faq',
    titleKey: '常见问题',
    icon: 'help',
    items: [
      { slug: 'general', titleKey: '通用' },
      { slug: 'billing', titleKey: '计费' },
      { slug: 'troubleshooting', titleKey: '故障排除' },
    ],
  },
  {
    id: 'changelog',
    titleKey: '更新日志',
    icon: 'file',
    items: [
      { slug: 'index', titleKey: '更新日志' },
    ],
  },
];

function getFirstPageForCategory(categoryId) {
  const category = docsNavCategories.find((c) => c.id === categoryId);
  if (!category || category.items.length === 0) return null;
  return { category: categoryId, slug: category.items[0].slug };
}

// --- Content Registry ---
// Static imports for all markdown content files
import en_quick_start_introduction from '@/content/docs/en/quick-start/introduction.md?raw';
import en_quick_start_installation from '@/content/docs/en/quick-start/installation.md?raw';
import en_quick_start_configuration from '@/content/docs/en/quick-start/configuration.md?raw';
import en_download_aiexcel from '@/content/docs/en/download/aiexcel.md?raw';
import en_download_browser_extension from '@/content/docs/en/download/browser-extension.md?raw';
import en_api_guide_authentication from '@/content/docs/en/api-guide/authentication.md?raw';
import en_api_guide_chat_completions from '@/content/docs/en/api-guide/chat-completions.md?raw';
import en_api_guide_models from '@/content/docs/en/api-guide/models.md?raw';
import en_api_guide_rate_limits from '@/content/docs/en/api-guide/rate-limits.md?raw';
import en_faq_general from '@/content/docs/en/faq/general.md?raw';
import en_faq_billing from '@/content/docs/en/faq/billing.md?raw';
import en_faq_troubleshooting from '@/content/docs/en/faq/troubleshooting.md?raw';
import en_changelog_index from '@/content/docs/en/changelog/index.md?raw';

import zh_quick_start_introduction from '@/content/docs/zh/quick-start/introduction.md?raw';
import zh_quick_start_installation from '@/content/docs/zh/quick-start/installation.md?raw';
import zh_quick_start_configuration from '@/content/docs/zh/quick-start/configuration.md?raw';
import zh_download_aiexcel from '@/content/docs/zh/download/aiexcel.md?raw';
import zh_download_browser_extension from '@/content/docs/zh/download/browser-extension.md?raw';
import zh_api_guide_authentication from '@/content/docs/zh/api-guide/authentication.md?raw';
import zh_api_guide_chat_completions from '@/content/docs/zh/api-guide/chat-completions.md?raw';
import zh_api_guide_models from '@/content/docs/zh/api-guide/models.md?raw';
import zh_api_guide_rate_limits from '@/content/docs/zh/api-guide/rate-limits.md?raw';
import zh_faq_general from '@/content/docs/zh/faq/general.md?raw';
import zh_faq_billing from '@/content/docs/zh/faq/billing.md?raw';
import zh_faq_troubleshooting from '@/content/docs/zh/faq/troubleshooting.md?raw';
import zh_changelog_index from '@/content/docs/zh/changelog/index.md?raw';

const contentMap = {
  'en_quick-start_introduction': en_quick_start_introduction,
  'en_quick-start_installation': en_quick_start_installation,
  'en_quick-start_configuration': en_quick_start_configuration,
  'en_download_aiexcel': en_download_aiexcel,
  'en_download_browser-extension': en_download_browser_extension,
  'en_api-guide_authentication': en_api_guide_authentication,
  'en_api-guide_chat-completions': en_api_guide_chat_completions,
  'en_api-guide_models': en_api_guide_models,
  'en_api-guide_rate-limits': en_api_guide_rate_limits,
  'en_faq_general': en_faq_general,
  'en_faq_billing': en_faq_billing,
  'en_faq_troubleshooting': en_faq_troubleshooting,
  'en_changelog_index': en_changelog_index,
  'zh_quick-start_introduction': zh_quick_start_introduction,
  'zh_quick-start_installation': zh_quick_start_installation,
  'zh_quick-start_configuration': zh_quick_start_configuration,
  'zh_download_aiexcel': zh_download_aiexcel,
  'zh_download_browser-extension': zh_download_browser_extension,
  'zh_api-guide_authentication': zh_api_guide_authentication,
  'zh_api-guide_chat-completions': zh_api_guide_chat_completions,
  'zh_api-guide_models': zh_api_guide_models,
  'zh_api-guide_rate-limits': zh_api_guide_rate_limits,
  'zh_faq_general': zh_faq_general,
  'zh_faq_billing': zh_faq_billing,
  'zh_faq_troubleshooting': zh_faq_troubleshooting,
  'zh_changelog_index': zh_changelog_index,
};

function getDocContent(lang, category, slug) {
  const normalizedLang = lang && lang.startsWith('zh') ? 'zh' : 'en';
  const key = `${normalizedLang}_${category}_${slug}`;
  if (contentMap[key]) return contentMap[key];
  const fallbackKey = `en_${category}_${slug}`;
  return contentMap[fallbackKey] ?? null;
}

// --- Docs Page ---
function Docs() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const isMobile = useIsMobile();
  const [search, setSearch] = useState('');
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // Parse URL: /docs/:category/:slug or /docs
  const pathParts = location.pathname.replace('/docs', '').split('/').filter(Boolean);
  const category = pathParts[0] || '';
  const slug = pathParts[1] || '';

  // Redirect if category given but no slug
  useEffect(() => {
    if (category && !slug) {
      const first = getFirstPageForCategory(category);
      if (first) {
        navigate(`/docs/${first.category}/${first.slug}`, { replace: true });
      }
    }
  }, [category, slug, navigate]);

  const filteredCategories = useMemo(() => {
    if (!search.trim()) return docsNavCategories;
    const query = search.toLowerCase();
    return docsNavCategories
      .map((cat) => {
        const filteredItems = cat.items.filter(
          (item) =>
            item.titleKey.toLowerCase().includes(query) ||
            cat.titleKey.toLowerCase().includes(query)
        );
        if (filteredItems.length === 0) return null;
        return { ...cat, items: filteredItems };
      })
      .filter(Boolean);
  }, [search]);

  const content = category && slug ? getDocContent(i18n.language, category, slug) : null;

  // Build sidebar nav items
  const navItems = useMemo(() => {
    const items = [];
    filteredCategories.forEach((cat) => {
      items.push({
        itemKey: `cat-${cat.id}`,
        text: t(cat.titleKey),
        items: cat.items.map((item) => ({
          itemKey: `${cat.id}/${item.slug}`,
          text: t(item.titleKey),
        })),
      });
    });
    return items;
  }, [filteredCategories, t]);

  const selectedKey = category && slug ? `${category}/${slug}` : '';

  const handleNavSelect = ({ itemKey }) => {
    if (itemKey.startsWith('cat-')) return;
    navigate(`/docs/${itemKey}`);
    if (isMobile) setSidebarOpen(false);
  };

  const categoryInfo = docsNavCategories.find((c) => c.id === category);
  const pageInfo = categoryInfo?.items.find((i) => i.slug === slug);

  const sidebarContent = (
    <div className='flex h-full flex-col'>
      <div className='p-3'>
        <Input
          prefix={<IconSearch />}
          placeholder={t('搜索文档...')}
          value={search}
          onChange={setSearch}
          showClear
        />
      </div>
      <div className='flex-1 overflow-y-auto'>
        <Nav
          items={navItems}
          selectedKeys={selectedKey ? [selectedKey] : []}
          onSelect={handleNavSelect}
          style={{ height: '100%' }}
          isCollapsed={false}
          header={{
            text: t('文档'),
          }}
        />
      </div>
    </div>
  );

  return (
    <div className='mt-[60px] flex' style={{ minHeight: 'calc(100vh - 60px)' }}>
      {/* Desktop Sidebar */}
      {!isMobile && (
        <aside
          className='hidden md:block shrink-0 border-r'
          style={{
            width: 260,
            position: 'sticky',
            top: 60,
            height: 'calc(100vh - 60px)',
            overflow: 'hidden',
          }}
        >
          {sidebarContent}
        </aside>
      )}

      {/* Mobile sidebar overlay */}
      {isMobile && sidebarOpen && (
        <div
          className='fixed inset-0 z-50 bg-black/30'
          onClick={() => setSidebarOpen(false)}
        >
          <aside
            className='absolute left-0 top-0 bottom-0 w-72 bg-white dark:bg-gray-900 shadow-lg overflow-hidden'
            onClick={(e) => e.stopPropagation()}
          >
            {sidebarContent}
          </aside>
        </div>
      )}

      {/* Content area */}
      <main className='min-h-[calc(100vh-60px)] flex-1'>
        {isMobile && (
          <div className='flex items-center border-b px-4 py-2'>
            <Button
              icon={<IconMenu />}
              theme='borderless'
              onClick={() => setSidebarOpen(true)}
            />
            <span className='ml-2 text-sm text-gray-500'>{t('文档')}</span>
          </div>
        )}

        {!category ? (
          /* Docs Home */
          <div className='mx-auto max-w-4xl px-4 py-12 md:px-8 md:py-16'>
            <div className='mb-10 text-center'>
              <h1 className='text-3xl font-bold tracking-tight md:text-4xl'>
                {t('文档')}
              </h1>
              <p className='mt-3 text-base text-gray-500'>
                {t('帮助您快速上手并充分利用平台功能。')}
              </p>
            </div>
            <div className='grid gap-4 sm:grid-cols-2'>
              {docsNavCategories.map((cat) => {
                const first = getFirstPageForCategory(cat.id);
                if (!first) return null;
                return (
                  <Link
                    key={cat.id}
                    to={`/docs/${first.category}/${first.slug}`}
                    className='group rounded-xl border p-5 transition-colors hover:bg-gray-50 dark:hover:bg-gray-800'
                  >
                    <div className='flex items-center gap-3'>
                      <span className='font-semibold'>{t(cat.titleKey)}</span>
                    </div>
                    <div className='mt-3 flex flex-wrap gap-1.5'>
                      {cat.items.map((item) => (
                        <span
                          key={item.slug}
                          className='rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-500 dark:bg-gray-700 dark:text-gray-400'
                        >
                          {t(item.titleKey)}
                        </span>
                      ))}
                    </div>
                  </Link>
                );
              })}
            </div>
          </div>
        ) : content ? (
          <div className='mx-auto max-w-4xl px-4 py-6 md:px-8'>
            {/* Breadcrumb */}
            {categoryInfo && pageInfo && (
              <div className='mb-4 flex items-center gap-1 text-sm text-gray-500'>
                <Link to='/docs' className='hover:text-gray-700 dark:hover:text-gray-300'>
                  {t('文档')}
                </Link>
                <span>/</span>
                <span>{t(categoryInfo.titleKey)}</span>
                {pageInfo.slug !== 'index' && (
                  <>
                    <span>/</span>
                    <span className='text-gray-700 dark:text-gray-300'>
                      {t(pageInfo.titleKey)}
                    </span>
                  </>
                )}
              </div>
            )}
            <div className='markdown-body'>
              <MarkdownContent content={content} />
            </div>
          </div>
        ) : (
          <div className='flex flex-1 flex-col items-center justify-center py-20'>
            <Empty
              image={<IllustrationConstruction style={{ width: 150, height: 150 }} />}
              title={t('页面未找到')}
              description={t('请求的文档页面不存在。')}
            />
          </div>
        )}
      </main>
    </div>
  );
}

export default Docs;
