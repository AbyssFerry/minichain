package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/abyssferry/minichain/llm"
	"github.com/abyssferry/minichain/utils"
)

// main 是程序入口，负责初始化配置、创建客户端并启动交互式命令行会话。
func main() {
	envMap, err := utils.LoadEnv(".env")
	if err != nil {
		log.Fatal(err)
	}

	customSystemPrompt := "你是我的小助手"
	customSystemPrompt = utils.GetEnv(envMap, "SYSTEM_PROMPT", customSystemPrompt)

	// requestTimeout 控制单轮模型请求的超时时间。
	requestTimeoutSeconds := utils.GetEnvInt(envMap, "REQUEST_TIMEOUT_SECONDS", 90)
	requestTimeout := time.Duration(requestTimeoutSeconds) * time.Second

	// maxReactRounds 控制单次 ReAct 推理允许的最大轮数。
	maxReactRounds := utils.GetEnvInt(envMap, "MAX_REACT_ROUNDS", 20)
	// contextTrimTokenThreshold 控制触发历史裁剪与摘要的 token 阈值。
	contextTrimTokenThreshold := utils.GetEnvInt(envMap, "CONTEXT_TRIM_TOKEN_THRESHOLD", 16000)
	// contextKeepRecentRounds 控制裁剪后保留的最近轮次数量。
	contextKeepRecentRounds := utils.GetEnvInt(envMap, "CONTEXT_KEEP_RECENT_ROUNDS", 5)

	model := utils.GetEnv(envMap, "MODEL", "")
	apiKey := utils.GetEnv(envMap, "API_KEY", "")
	baseURL := utils.GetEnv(envMap, "BASE_URL", "")
	temperature := utils.GetEnvFloat64Ptr(envMap, "TEMPERATURE")
	topP := utils.GetEnvFloat64Ptr(envMap, "TOP_P")
	maxTokens := utils.GetEnvIntPtr(envMap, "MAX_TOKENS")
	presencePenalty := utils.GetEnvFloat64Ptr(envMap, "PRESENCE_PENALTY")
	frequencyPenalty := utils.GetEnvFloat64Ptr(envMap, "FREQUENCY_PENALTY")
	seed := utils.GetEnvIntPtr(envMap, "SEED")
	stop := utils.GetEnvCSV(envMap, "STOP", "")

	debugMessages := utils.GetEnvBool(envMap, "DEBUG_MESSAGES", false)
	debugRequestParams := utils.GetEnvBool(envMap, "DEBUG_REQUEST_PARAMS", false)
	thinkingEnabled := utils.GetEnvBool(envMap, "ENABLE_THINKING", false)
	thinking := &llm.ThinkingConfig{Type: "disabled"}
	if thinkingEnabled {
		thinking.Type = "enabled"
	}

	chatModel, err := llm.InitChatModel(llm.ChatModelOptions{
		Model:                     model,
		SystemPrompt:              customSystemPrompt,
		APIKey:                    apiKey,
		BaseURL:                   baseURL,
		Temperature:               temperature,
		TopP:                      topP,
		MaxTokens:                 maxTokens,
		Stop:                      stop,
		PresencePenalty:           presencePenalty,
		FrequencyPenalty:          frequencyPenalty,
		Seed:                      seed,
		ContextTrimTokenThreshold: contextTrimTokenThreshold,
		ContextKeepRecentRounds:   contextKeepRecentRounds,

		RequestTimeout:     &requestTimeout,
		DebugMessages:      debugMessages,
		DebugRequestParams: debugRequestParams,
		Thinking:           thinking,
	})
	if err != nil {
		log.Fatal(err)
	}

	registry := llm.NewToolRegistry()
	if err := registry.RegisterFromHandler("get_current_time", "当你想知道现在时间时非常有用。", getCurrentTime); err != nil {
		log.Fatal(err)
	}
	if err := registry.RegisterFromHandler("get_current_weather", "当你想查询指定城市天气时非常有用。", getCurrentWeather); err != nil {
		log.Fatal(err)
	}

	agent, err := llm.CreateAgent(llm.AgentOptions{
		Model:                     model,
		SystemPrompt:              customSystemPrompt,
		APIKey:                    apiKey,
		BaseURL:                   baseURL,
		Temperature:               temperature,
		TopP:                      topP,
		MaxTokens:                 maxTokens,
		Stop:                      stop,
		PresencePenalty:           presencePenalty,
		FrequencyPenalty:          frequencyPenalty,
		Seed:                      seed,
		MaxReactRounds:            maxReactRounds,
		Tools:                     registry,
		ContextTrimTokenThreshold: contextTrimTokenThreshold,
		ContextKeepRecentRounds:   contextKeepRecentRounds,
		RequestTimeout:            &requestTimeout,
		DebugMessages:             debugMessages,
		DebugRequestParams:        debugRequestParams,
		Thinking:                  thinking,
	})
	if err != nil {
		log.Fatal(err)
	}

	mode := "chat"
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("minichain 风格 Go CLI 已启动")
	fmt.Printf("示例: 本程序会在初始化时注入用户自定义系统提示词: %s\n", customSystemPrompt)
	fmt.Println("命令: /mode chat | /mode stream | /mode react | /clear | /help | /exit")

	for {
		fmt.Printf("[%s] 你: ", mode)
		if !scanner.Scan() {
			fmt.Println("\n输入结束，程序退出")
			return
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			// 斜杠开头视为控制命令，不进入模型对话流程。
			nextMode, shouldExit, cleared := handleCommand(input, mode)
			if shouldExit {
				fmt.Println("已退出")
				return
			}
			mode = nextMode
			if cleared {
				chatModel.Reset()
				agent.Reset()
				fmt.Println("上下文已清空")
			}
			continue
		}

		messages := []llm.Message{
			{Role: "user", Content: input},
		}

		switch mode {
		case "chat":
			result, err := chatModel.Invoke(llm.InvokeInput{Messages: messages})
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}
			fmt.Printf("助手: %s\n", result.Content)
		case "stream":
			fmt.Print("助手(流式): ")
			result, err := chatModel.Stream(llm.InvokeInput{Messages: messages})
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}
			var contentBuilder strings.Builder
			for event := range result.Events {
				if event.Type == "content" && event.Content != "" {
					fmt.Print(event.Content)
					contentBuilder.WriteString(event.Content)
				}
			}
			summary, waitErr := result.Wait()
			if waitErr != nil {
				fmt.Printf("错误: %v\n", waitErr)
				continue
			}
			if summary.Content != "" && summary.Content != contentBuilder.String() {
				contentBuilder.Reset()
				contentBuilder.WriteString(summary.Content)
			}
			fmt.Printf("\n助手(完整): %s\n", contentBuilder.String())
		case "react":
			fmt.Print("助手(流式): ")
			result, err := agent.Stream(llm.InvokeInput{Messages: messages})
			if err != nil {
				fmt.Printf("错误: %v\n", err)
				continue
			}
			var contentBuilder strings.Builder
			for event := range result.Events {
				switch event.Type {
				case "content":
					fmt.Print(event.Content)
					contentBuilder.WriteString(event.Content)
				case "tool_start":
					fmt.Printf("\n[工具] 开始 %s 参数=%s\n", event.ToolName, event.RawArguments)
				case "tool_end":
					fmt.Printf("[工具] 完成 %s 参数=%s 输出=%s\n", event.ToolName, event.RawArguments, event.Content)
				case "error":
					fmt.Printf("\n[流式错误] %s\n", event.Content)
				}
			}
			summary, waitErr := result.Wait()
			if waitErr != nil {
				fmt.Printf("错误: %v\n", waitErr)
				continue
			}
			if summary.Content != "" && summary.Content != contentBuilder.String() {
				contentBuilder.Reset()
				contentBuilder.WriteString(summary.Content)
			}
			fmt.Printf("\n助手(完整): %s\n", contentBuilder.String())
		default:
			fmt.Printf("未知模式: %s\n", mode)
		}
	}
}

// getCurrentTimeArgs 是获取时间工具入参。
type getCurrentTimeArgs struct{}

// getCurrentTime 返回当前本地时间。
func getCurrentTime(_ getCurrentTimeArgs) (string, error) {
	time.Sleep(1 * time.Second)
	return fmt.Sprintf("当前时间：%s。", time.Now().Format("2006-01-02 15:04:05")), nil
}

// getCurrentWeatherArgs 是天气工具入参。
type getCurrentWeatherArgs struct {
	// Location 是查询天气的城市或区县。
	Location string `json:"location" tool:"desc=城市或县区，比如北京市、杭州市、余杭区等。;required"`
	// Unit 是温度单位，支持 c 或 f。
	Unit string `json:"unit,omitempty" tool:"desc=温度单位，c表示摄氏度，f表示华氏度。;default=c;enum=c|f"`
	// Days 是天气预报天数。
	Days int `json:"days,omitempty" tool:"desc=天气预报天数，范围1-7。;default=1"`
}

// getCurrentWeather 根据位置返回模拟天气。
func getCurrentWeather(arguments getCurrentWeatherArgs) (string, error) {
	time.Sleep(2 * time.Second)
	location := strings.TrimSpace(arguments.Location)
	if location == "" {
		return "", errors.New("missing argument: location")
	}
	unit := strings.ToLower(strings.TrimSpace(arguments.Unit))
	if unit == "" {
		unit = "c"
	}
	if unit != "c" && unit != "f" {
		return "", errors.New("invalid argument: unit must be c or f")
	}
	days := arguments.Days
	if days <= 0 {
		days = 1
	}
	if days > 7 {
		return "", errors.New("invalid argument: days must be between 1 and 7")
	}

	weather := []string{"晴天", "多云", "雨天"}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	temp := r.Intn(16) + 18
	if unit == "f" {
		temp = temp*9/5 + 32
	}
	return fmt.Sprintf("%s未来%d天首日是%s，温度约%d°%s。", location, days, weather[r.Intn(len(weather))], temp, strings.ToUpper(unit)), nil
}

// handleCommand 解析并执行 CLI 命令，返回下一模式、是否退出和是否清空上下文。
func handleCommand(input string, mode string) (nextMode string, shouldExit bool, cleared bool) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return mode, false, false
	}

	switch parts[0] {
	case "/exit", "/quit":
		return mode, true, false
	case "/help":
		fmt.Println("命令: /mode chat|stream|react, /clear, /help, /exit")
		return mode, false, false
	case "/clear":
		return mode, false, true
	case "/mode":
		if len(parts) < 2 {
			fmt.Println("用法: /mode chat|stream|react")
			return mode, false, false
		}
		target := strings.ToLower(parts[1])
		if target != "chat" && target != "stream" && target != "react" {
			fmt.Println("仅支持: chat, stream, react")
			return mode, false, false
		}
		fmt.Printf("已切换模式: %s\n", target)
		return target, false, false
	default:
		fmt.Println("未知命令，可输入 /help 查看帮助")
		return mode, false, false
	}
}
