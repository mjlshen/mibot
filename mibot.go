package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/nlopes/slack"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	slackToken := os.Getenv("SLACK_TOKEN")
	kubeconfigPath := os.Getenv("KUBECONFIG")

	kubeconfig := flag.String("kubeconfig", kubeconfigPath, "absolute path to the kubeconfig file")
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Regular expressions for the bot to match against
	getDeployRegexp := regexp.MustCompile(`k(ubectl)? get deploy(ment)?(s)? -n (?P<namespace>.*)`)
	getPodRegexp := regexp.MustCompile(`k(ubectl)? get po(d)?(s)? -n (?P<namespace>.*)`)

	// Initialize Slack bot
	api := slack.New(
		slackToken,
		slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stdout, "slack-bot: ", log.Lshortfile|log.LstdFlags)),
	)

	// Start RTM connection
	rtm := api.NewRTM()
	go rtm.ManageConnection()

	for msg := range rtm.IncomingEvents {
		//fmt.Print("Event Received: %s\n, msg.Data")
		switch ev := msg.Data.(type) {
		case *slack.HelloEvent:
			// Ignore hello

		case *slack.ConnectedEvent:
			// Ignore when the bot first connects

		case *slack.MessageEvent:
			botTagString := fmt.Sprintf("<@%s>", rtm.GetInfo().User.ID)
			if !strings.Contains(ev.Msg.Text, botTagString) {
				continue
			}

			if getDeployRegexp.MatchString(ev.Msg.Text) {
				args := regexpSubexpMatch(getDeployRegexp, ev.Msg.Text)
				deploymentsClient := clientset.AppsV1().Deployments(args["namespace"])

				var deployments strings.Builder
				list, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					panic(err)
				}
				deployments.WriteString("```\n")
				for _, d := range list.Items {
					deployments.WriteString(d.Name + "\n")
				}
				deployments.WriteString("```")
				fmt.Printf(deployments.String())
				rtm.SendMessage(rtm.NewOutgoingMessage(deployments.String(), ev.Channel))
			} else if getPodRegexp.MatchString(ev.Msg.Text) {
				args := regexpSubexpMatch(getPodRegexp, ev.Msg.Text)
				podsClient := clientset.CoreV1().Pods(args["namespace"])

				var pods strings.Builder
				list, err := podsClient.List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					panic(err)
				}
				pods.WriteString("```\n")
				for _, po := range list.Items {
					runningContainers := 0
					for _, container := range po.Status.ContainerStatuses {
						if container.State.Running != nil {
							runningContainers++
						}
					}
					pods.WriteString(po.Name + "\t" + string(po.Status.Phase) + "\t" + strconv.Itoa(runningContainers) + "/" + strconv.Itoa(len(po.Status.ContainerStatuses)) + "\n")
				}
				pods.WriteString("```")
				fmt.Printf(pods.String())
				rtm.SendMessage(rtm.NewOutgoingMessage(pods.String(), ev.Channel))
			} else if strings.Contains(ev.Msg.Text, "help") {
				rtm.SendMessage(rtm.NewOutgoingMessage("```\nkubectl get deploy -n $namespace\nkubectl get po -n $namespace\n```", ev.Channel))
			} else {
				rtm.SendMessage(rtm.NewOutgoingMessage("I'm mibot. I'm alive, but idk what you want from me! Try help? :narwhal-dancing:", ev.Channel))
			}

			// pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
			// if err != nil {
			// 	panic(err.Error())
			// }
			// rtm.SendMessage(rtm.NewOutgoingMessage("There are %s pods in the cluster", strconv.Itoa(len(pods.Items))))
			// fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))
			// rtm.SendMessage(rtm.NewOutgoingMessage("I'm mibot. I'm alive!", ev.Channel))

		case *slack.PresenceChangeEvent:
			fmt.Printf("Presence Change: %v\n", ev)

		case *slack.LatencyReport:
			fmt.Printf("Current latency: %v\n", ev.Value)

		case *slack.DesktopNotificationEvent:
			fmt.Printf("Desktop Notification: %v\n", ev)

		case *slack.RTMError:
			fmt.Printf("Error: %s\n", ev.Error())

		case *slack.InvalidAuthEvent:
			fmt.Printf("Invalid credentials")
			return

		default:

			// Ignore other events..
			// fmt.Printf("Unexpected: %v\n", msg.Data)
		}
	}
}

func regexpSubexpMatch(r *regexp.Regexp, str string) map[string]string {
	match := r.FindStringSubmatch(str)
	subexpMatchMap := make(map[string]string)
	for i, name := range r.SubexpNames() {
		if i != 0 {
			subexpMatchMap[name] = match[i]
		}
	}

	return subexpMatchMap
}
