package spot

import (
	"container/list"
	"context"
	"log"
	"sync"
	"time"

	client "github.com/adshao/go-binance/v2"
)

type SpotKlines struct {
	mutex sync.RWMutex

	onceStart  sync.Once
	onceInited sync.Once
	onceStop   sync.Once

	inited chan struct{}
	stopC  chan struct{}

	si         SymbolInterval
	klines     *list.List
	updateTime time.Time
}

func NewSpotKlines(si SymbolInterval) *SpotKlines {
	return &SpotKlines{
		si:     si,
		stopC:  make(chan struct{}, 1),
		inited: make(chan struct{}),
	}
}

func (s *SpotKlines) Start() {
	go func() {
		for delay := 1; ; delay *= 2 {
			if delay > 60 {
				delay = 60
			}
			time.Sleep(time.Second * time.Duration(delay-1))

			s.mutex.Lock()
			s.klines = nil
			s.mutex.Unlock()

			client.WebsocketKeepalive = true
			doneC, stopC, err := client.WsKlineServe(s.si.Symbol, s.si.Interval, s.wsHandler, s.errHandler)
			if err != nil {
				continue
			}

			delay = 1
			select {
			case stopC <- <-s.stopC:
				return
			case <-doneC:
			}
		}
	}()
}

func (s *SpotKlines) Stop() {
	s.onceStop.Do(func() {
		s.stopC <- struct{}{}
	})
}

func (s *SpotKlines) wsHandler(event *client.WsKlineEvent) {
	defer s.mutex.Unlock()
	s.mutex.Lock()

	if s.klines == nil {
		for delay := 1; ; delay *= 2 {
			if delay > 60 {
				delay = 60
			}
			time.Sleep(time.Second * time.Duration(delay-1))

			klines, err := client.NewClient("", "").NewKlinesService().
				Symbol(s.si.Symbol).Interval(s.si.Interval).Limit(1000).
				Do(context.Background())
			if err != nil {
				log.Printf("%s.Get initialization klines error!Error:%s", s.si, err)
			}

			s.klines = list.New()
			for _, v := range klines {
				s.klines.PushBack(v)
			}
			break
		}
	}

	// Merge kline
	kline := &client.Kline{
		OpenTime:                 event.Kline.StartTime,
		Open:                     event.Kline.Open,
		High:                     event.Kline.High,
		Low:                      event.Kline.Low,
		Close:                    event.Kline.Close,
		Volume:                   event.Kline.Volume,
		CloseTime:                event.Kline.EndTime,
		QuoteAssetVolume:         event.Kline.QuoteVolume,
		TradeNum:                 event.Kline.TradeNum,
		TakerBuyBaseAssetVolume:  event.Kline.ActiveBuyVolume,
		TakerBuyQuoteAssetVolume: event.Kline.ActiveBuyQuoteVolume,
	}

	if s.klines.Back().Value.(*client.Kline).OpenTime < kline.OpenTime {
		s.klines.PushBack(kline)
	} else if s.klines.Back().Value.(*client.Kline).OpenTime == kline.OpenTime {
		s.klines.Back().Value = kline
	}

	for s.klines.Len() > 1000 {
		s.klines.Remove(s.klines.Front())
	}
	s.updateTime = time.Now()

	s.onceInited.Do(func() {
		close(s.inited)
	})
}

func (s *SpotKlines) errHandler(err error) {
	log.Printf("%s.Kline websocket throw error!Error:%s", s.si, err)
}

func (s *SpotKlines) GetKlines() []client.Kline {
	<-s.inited

	defer s.mutex.RUnlock()
	s.mutex.RLock()

	// if time.Now().Sub(s.updateTime).Seconds() > 5 {
	// 	return nil
	// }
	if s.klines == nil {
		return nil
	}

	res := make([]client.Kline, s.klines.Len())
	for elems := s.klines.Front(); elems != nil; elems = elems.Next() {
		res = append(res, *(elems.Value.(*client.Kline)))
	}

	return res
}