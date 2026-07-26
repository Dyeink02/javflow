/**
 * @file parser.ts
 * @description 解析器模块，用于解析页面内容和提取所需信息。
 * @module parser
 * @requires cheerio - 用于解析HTML的库。
 * @exports parsePageLinks - 解析页面中的电影链接。
 * @exports parseMetadata - 解析页面中的元数据。
 * @exports parseCategories - 解析页面中的分类信息。
 * @exports parseActress - 解析页面中的演员信息。
 * @exports parseFilmData - 解析页面中的影片数据。
 * @author raawaa
 */

import { Config, Metadata, FilmData } from '../types/interfaces';
import logger from './logger';

class Parser {
  private config: Config;

  public constructor(config: Config) {
    this.config = config;
  }

  /**
   * 解析页面中的电影链接
   * @param {string} html - 页面HTML内容
   * @returns {Array<string>} 电影详情页链接数组
   */
  static parsePageLinks(html: string): Array<string> {
    // 检查页面内容是否为空
    if (!html || html.length === 0) {
      logger.warn('parsePageLinks: 接收到空的HTML内容');
      return [];
    }

    const $ = require('cheerio').load(html);
    
    const links: string[] = Array.from(
      new Set(
        $('a.movie-box')
          .map((i: number, el: cheerio.Element) => $(el).attr('href'))
          .get()
          .map((link: unknown) => String(link || '').trim())
          .filter(Boolean)
      )
    );
    
    logger.debug(`解析到 ${links.length} 个影片链接`);
    if (links.length === 0) {
      // 只在调试模式下输出完整HTML或其片段，避免日志过长
      logger.debug('页面中未找到影片链接，页面内容片段 (前1000字符):');
      logger.debug(html.substring(0, 1000));
    }
    
    return links;
  }


  /**
   * 解析页面中的元数据
   * @param {string} html - 页面 HTML 内容
   * @returns {Metadata} 包含影片元数据的对象
   * @throws {Error} 当无法从脚本中解析出所需元数据时抛出错误
   * @description 从页面 HTML 内容中提取影片的 gid、uc、img、标题、分类和演员信息
   */
  static parseMetadata(html: string) {
    if (!html || typeof html !== 'string') {
      logger.warn('parseMetadata: HTML内容为空或不是字符串');
      throw new Error('Invalid HTML content for metadata parsing');
    }

    if (html.length === 0) {
      logger.warn('parseMetadata: HTML内容长度为0');
      throw new Error('Empty HTML content for metadata parsing');
    }

    const $ = require('cheerio').load(html);
    const scripts = $('script', 'body')
      .map((_: number, el: cheerio.Element) => $(el).html() || '')
      .get()
      .filter(Boolean);

    logger.debug(`parseMetadata: 页面中找到 ${scripts.length} 个script标签`);

    const primaryScript = scripts.find((item: string) =>
      /(?:\bgid\b|\buc\b|\bimg\b|uncledatoolsbyajax|sample_dmm)/i.test(item)
    );
    const metadataSources = [primaryScript || '', ...scripts, html].filter(Boolean);

    const gid = this.extractMetadataField(metadataSources, [
      /\bgid\b\s*[:=]\s*['"]?(\d+)['"]?/i,
      /["']gid["']\s*:\s*["']?(\d+)["']?/i,
      /\bgid\b\s*[:=]\s*parseInt\(\s*['"]?(\d+)['"]?/i,
      /[?&]gid=(\d+)/i,
      /data-gid\s*=\s*["'](\d+)["']/i
    ]);
    const uc = this.extractMetadataField(metadataSources, [
      /\buc\b\s*[:=]\s*['"]?(\d+)['"]?/i,
      /["']uc["']\s*:\s*["']?(\d+)["']?/i,
      /\buc\b\s*[:=]\s*parseInt\(\s*['"]?(\d+)['"]?/i,
      /[?&]uc=(\d+)/i,
      /data-uc\s*=\s*["'](\d+)["']/i
    ]);
    const img =
      this.extractMetadataField(metadataSources, [
        /\bimg\b\s*[:=]\s*['"]([^'"]+)['"]/i,
        /["']img["']\s*:\s*["']([^"']+)["']/i,
        /[?&]img=([^&"'>\s]+)/i,
        /data-img\s*=\s*["']([^"']+)["']/i
      ])?.replace(/\\\//g, '/') ||
      $('.bigImage img').attr('src') ||
      $('a.bigImage img').attr('src') ||
      $('meta[property="og:image"]').attr('content') ||
      '';

    logger.debug(`解析脚本内容: gidMatch=${Boolean(gid)}, ucMatch=${Boolean(uc)}, imgMatch=${Boolean(img)}`);
    logger.debug(`主脚本长度: ${(primaryScript || '').length} 字符`);

    if (!gid || !uc || !img) {
      logger.warn('parseMetadata: 无法从页面中解析出完整元数据');
      logger.debug(`主脚本内容: ${(primaryScript || '').substring(0, 500)}`);
      logger.debug(`尝试替代解析: altGidMatch=${Boolean(gid)}, altUcMatch=${uc || null}`);
      throw new Error('Failed to parse required metadata from page');
    }
    
    const metadata: Metadata = {
      gid,
      uc,
      img,
      title:
        $('h3').first().text().trim() ||
        $('meta[property="og:title"]').attr('content')?.trim() ||
        $('title').text().trim(),
      category: this.parseCategories($),
      actress: this.parseActress($)
    };
    
    logger.debug(`解析到影片元数据: 标题=${metadata.title}, gid=${metadata.gid}, uc=${metadata.uc}`);
    return metadata;
  }

  private static extractMetadataField(sources: string[], patterns: RegExp[]): string | null {
    for (const source of sources) {
      for (const pattern of patterns) {
        const match = pattern.exec(source);
        if (match?.[1]) {
          return match[1].trim();
        }
      }
    }

    return null;
  }


  /**
   * 解析HTML内容中的影片分类信息
   * @param {any} $ - Cheerio对象或包含分类信息的HTML字符串
   * @returns {Array<string>} 返回分类名称的数组
   * @description 从HTML中提取所有位于<span class="genre">标签内，
   * 且嵌套在<label><a>结构中的文本内容作为分类名称
   */
  static parseCategories($: any): Array<string> {
    // 如果传入的是HTML字符串，则加载为Cheerio对象
    if (typeof $ === 'string') {
      if (!$ || $.length === 0) {
        logger.warn('parseCategories: HTML内容为空或不是字符串');
        return [];
      }
      $ = require('cheerio').load($);
    } else if (!$ || typeof $ !== 'function') {
      logger.warn('parseCategories: 传入的不是有效的Cheerio对象或HTML字符串');
      return [];
    }

    // 检查页面是否包含关键HTML结构
    const hasGenreElements = $('span.genre').length > 0;
    const hasLabelElements = $('span.genre label').length > 0;
    const hasAnchorElements = $('span.genre a').length > 0;

    if (!hasGenreElements) {
      logger.debug('parseCategories: 页面中没有找到 span.genre 元素');
      logger.debug(`页面片段: ${$.html ? $.html().substring(0, 500) : '无法获取HTML内容'}`);
    }

    if (!hasLabelElements && hasGenreElements) {
      logger.debug('parseCategories: 找到 span.genre 但没有找到 label 元素');
    }

    if (!hasAnchorElements && hasLabelElements) {
      logger.debug('parseCategories: 找到 label 但没有找到 a 元素');
    }

    const categories = $('span.genre label a').map((i: number, el: cheerio.Element) => $(el).text()).get();
    logger.debug(`解析到 ${categories.length} 个分类: ${categories.join(', ')}`);

    if (categories.length === 0 && hasGenreElements) {
      logger.debug('parseCategories: 页面包含genre元素但未能解析到分类文本');
      // 输出一些元素用于调试
      const sampleGenre = $('span.genre').first();
      if (sampleGenre.length > 0) {
        logger.debug(`第一个genre元素HTML: ${sampleGenre.html()}`);
      }
    }

    return categories;
  }


  /**
   * 解析HTML内容中的女演员信息
   * @param {any} $ - Cheerio对象或包含演员信息的HTML字符串
   * @returns {Array<string>} 返回女演员名称的数组
   * @description 从HTML中提取所有位于.star-name .a标签内的文本内容作为女演员名称
   */
  static parseActress($: any): Array<string> {
    // 如果传入的是HTML字符串，则加载为Cheerio对象
    if (typeof $ === 'string') {
      if (!$ || $.length === 0) {
        logger.warn('parseActress: HTML内容为空或不是字符串');
        return [];
      }
      $ = require('cheerio').load($);
    } else if (!$ || typeof $ !== 'function') {
      logger.warn('parseActress: 传入的不是有效的Cheerio对象或HTML字符串');
      return [];
    }

    // 检查页面是否包含关键HTML结构
    const hasStarNameElements = $('.star-name').length > 0;
    const hasStarBoxElements = $('.star-box').length > 0;
    const hasAnchorElements = $('.star-name a').length > 0;
    const hasActressSection = $.html ? ($.html().includes('演員') || $.html().includes('女優')) : false;

    if (!hasActressSection) {
      logger.debug('parseActress: 页面中没有找到演员相关文字');
    }

    if (!hasStarNameElements && hasStarBoxElements) {
      logger.debug('parseActress: 找到 star-box 但没有找到 star-name 元素');
    }

    if (!hasAnchorElements && hasStarNameElements) {
      logger.debug('parseActress: 找到 star-name 但没有找到 a 元素');
    }

    const actresses = $('.star-name a').map((i: number, el: cheerio.Element) => $(el).text()).get();
    logger.debug(`解析到 ${actresses.length} 个演员: ${actresses.join(', ')}`);

    if (actresses.length === 0 && hasStarBoxElements) {
      logger.debug('parseActress: 页面包含star-box元素但未能解析到演员信息');
      // 输出一些元素用于调试
      const sampleStarBox = $('.star-box').first();
      if (sampleStarBox.length > 0) {
        logger.debug(`第一个star-box元素HTML: ${sampleStarBox.html()}`);
      }
    }

    return actresses;
  }

  /**
   * 将解析的元数据和磁力链接组合成影片数据对象
   * @param {Metadata} metadata - 包含影片元数据的对象
   * @param {string} magnet - 影片的磁力链接，如果没有则为null
   * @param {string} link - 影片详情页链接
   * @returns {FilmData} 返回包含完整影片数据的对象
   * @description 将影片标题、分类、演员信息和磁力链接组合成一个完整的数据对象，
   * 便于后续处理和存储
   */
  static parseFilmData(metadata: Metadata, link: string): FilmData {
    const actressCount = Array.isArray(metadata.actress) ? metadata.actress.length : 0;
    const filmData: FilmData = {
      title: metadata.title,
      sourceLink: link,
      category: metadata.category,
      actress: metadata.actress,
      actressCount
    };

    if (typeof metadata.img === 'string' && metadata.img.trim()) {
      filmData.coverImage = metadata.img.trim();
    }

    return filmData;
  }

  /**
   * 提取防屏蔽地址
   * @param {string} html - 页面HTML内容
   * @returns {Array<string>} 防屏蔽地址数组
   * @description 从页面中提取所有包含防屏蔽地址的链接
   */
  static extractAntiBlockUrls(html: string): Array<string> {
    const $ = require('cheerio').load(html);
    const antiBlockUrls: Array<string> = [];

    // 定位到包含防屏蔽地址的警告框
    const alertBox = $('.alert.alert-info');
    
    // 遍历所有包含防屏蔽地址的列
    alertBox.find('.col-xs-12.col-md-6.col-lg-3.text-center').each((i: number, elem: cheerio.Element) => {
        const strongText = $(elem).find('strong').text().trim();
        
        // 验证是否是防屏蔽地址条目
        if (strongText.includes('防屏蔽地址')) {
            const url = $(elem).find('a').attr('href').trim();
            antiBlockUrls.push(url);
        }
    });

    return antiBlockUrls;
}

}

export default Parser;
