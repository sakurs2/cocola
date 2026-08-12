import agentsEn from "@/messages/en/agents.json";
import authEn from "@/messages/en/auth.json";
import adminEn from "@/messages/en/admin.json";
import chatEn from "@/messages/en/chat.json";
import commonEn from "@/messages/en/common.json";
import connectorsEn from "@/messages/en/connectors.json";
import errorsEn from "@/messages/en/errors.json";
import navigationEn from "@/messages/en/navigation.json";
import profileEn from "@/messages/en/profile.json";
import projectsEn from "@/messages/en/projects.json";
import skillsEn from "@/messages/en/skills.json";
import tasksEn from "@/messages/en/tasks.json";
import wikiEn from "@/messages/en/wiki.json";
import workspaceEn from "@/messages/en/workspace.json";

import agentsZh from "@/messages/zh-CN/agents.json";
import authZh from "@/messages/zh-CN/auth.json";
import adminZh from "@/messages/zh-CN/admin.json";
import chatZh from "@/messages/zh-CN/chat.json";
import commonZh from "@/messages/zh-CN/common.json";
import connectorsZh from "@/messages/zh-CN/connectors.json";
import errorsZh from "@/messages/zh-CN/errors.json";
import navigationZh from "@/messages/zh-CN/navigation.json";
import profileZh from "@/messages/zh-CN/profile.json";
import projectsZh from "@/messages/zh-CN/projects.json";
import skillsZh from "@/messages/zh-CN/skills.json";
import tasksZh from "@/messages/zh-CN/tasks.json";
import wikiZh from "@/messages/zh-CN/wiki.json";
import workspaceZh from "@/messages/zh-CN/workspace.json";

export const englishMessages = {
  admin: adminEn,
  agents: agentsEn,
  auth: authEn,
  chat: chatEn,
  common: commonEn,
  connectors: connectorsEn,
  errors: errorsEn,
  navigation: navigationEn,
  profile: profileEn,
  projects: projectsEn,
  skills: skillsEn,
  tasks: tasksEn,
  wiki: wikiEn,
  workspace: workspaceEn,
};

export type AppMessages = typeof englishMessages;

const simplifiedChineseMessages = {
  admin: adminZh,
  agents: agentsZh,
  auth: authZh,
  chat: chatZh,
  common: commonZh,
  connectors: connectorsZh,
  errors: errorsZh,
  navigation: navigationZh,
  profile: profileZh,
  projects: projectsZh,
  skills: skillsZh,
  tasks: tasksZh,
  wiki: wikiZh,
  workspace: workspaceZh,
} satisfies AppMessages;

export const messagesByLocale = {
  en: englishMessages,
  "zh-CN": simplifiedChineseMessages,
} as const;
